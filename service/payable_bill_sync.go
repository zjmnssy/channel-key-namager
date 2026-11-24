package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	payableBillMaxConcurrentSites = 4
	// payableBillPullTime 是北京时间每天开始拉取的时刻（距 0 点），拉到的记为昨天的账单。
	payableBillPullTime = 10 * time.Minute
	// payableChannelNameSize 是明细表 remote_channel_name 列的长度。
	payableChannelNameSize = 255
)

// RunPayableBillScan 在 now 扫描一次，所有到期的站点进度处理完后返回。
// 先把已删除站点未完成的进度改为失败，并补正进度都已结束却仍为 running 的任务；
// 北京时间过了当天 00:10 时创建账单日期为昨天的拉取任务（已有则跳过）；
// 再给每个到期的进度一个回合，最多 payableBillMaxConcurrentSites 个站点同时进行。
func RunPayableBillScan(ctx context.Context, now time.Time) {
	defer func() {
		if recovered := recover(); recovered != nil {
			common.SysError(common.LogText("payable bill scan panicked: %v", recovered))
		}
	}()

	if err := model.FailPayablePullsOfDeletedSites(model.DB, now.Unix()); err != nil {
		common.SysError(common.LogText("payable bill scan could not fail the pulls of deleted sites: %v", err))
	}
	if err := model.RefreshSettledPayableTasks(now.Unix()); err != nil {
		common.SysError(common.LogText("payable bill scan could not refresh the settled tasks: %v", err))
	}
	today := receivableBeijingDay(now)
	if !now.Before(today.Add(payableBillPullTime)) {
		sites, err := model.ListReceivableSyncSites()
		if err == nil {
			_, err = model.CreatePayablePullTask(model.ReceivableBillDate(today.AddDate(0, 0, -1)), sites, now.Unix())
		}
		if err != nil {
			common.SysError(common.LogText("payable bill scan could not create the pull task: %v", err))
		}
	}
	pulls, err := model.DuePayablePulls(now.Unix())
	if err != nil {
		common.SysError(common.LogText("payable bill scan failed: %v", err))
		return
	}

	var wg sync.WaitGroup
	slots := make(chan struct{}, payableBillMaxConcurrentSites)
	for _, pull := range pulls {
		slots <- struct{}{}
		wg.Add(1)
		gopool.Go(func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					common.SysError(common.LogText("payable bill site pull panicked: pull=%d error=%v", pull.Id, recovered))
				}
				<-slots
				wg.Done()
			}()
			if err := pullPayableSite(ctx, now.Unix(), pull.Id); err != nil {
				common.SysError(common.LogText("payable bill pull failed: pull=%d error=%v", pull.Id, err))
			}
		})
	}
	wg.Wait()
}

// pullPayableSite 是一个站点进度在本次扫描中的回合：从进度记下的页码起逐页拉取，每页认领一次租约、请求一次、落库一次。
// 请求失败（不论能否重试）、进度结束、租约丢失或拉满 channelPushMaxListPages 页时结束，剩下的等下一次扫描，
// 所以出错的站点每次扫描只收到一个失败的请求。返回的是数据库错误；没来得及释放的租约到期后自然失效。
func pullPayableSite(ctx context.Context, now int64, pullID int) error {
	for attempt := range channelPushMaxListPages {
		lease, claimed, err := model.ClaimPayablePull(pullID, now)
		if err != nil || !claimed {
			return err
		}
		// 认领后再读进度：页码、尝试次数和地址快照以认领时为准，站点编辑可能刚把它重置。
		var pull model.PayablePullSite
		if err := model.DB.First(&pull, pullID).Error; err != nil {
			return err
		}
		if attempt == 0 {
			if err := model.StartPayableTask(pull.TaskId, now); err != nil {
				return err
			}
		}
		site, err := model.GetChannelPushSite(pull.SiteId)
		if errors.Is(err, model.ErrThirdPartySiteNotFound) {
			return nil // 站点刚被删除，下一次扫描把进度改为失败
		}
		if err != nil {
			return err
		}
		goOn, err := pullPayablePage(ctx, now, pull, lease, site)
		if err != nil || !goOn {
			return err
		}
	}
	return nil
}

// pullPayablePage 是一次尝试：向站点请求进度的下一页，把结果连同本页新出现渠道的明细一起落库，返回回合是否继续。
// 一遍拉完、已写入的渠道仍少于站点报的总数时，从第 1 页补扫一遍；补扫后仍对不上，以已拉到的为准结束并记下说明。
func pullPayablePage(ctx context.Context, now int64, pull model.PayablePullSite, lease int64, site *model.ThirdPartySite) (bool, error) {
	page := NewChannelPushClient(site, PayableBillClientOptions).ReadChannelPage(ctx, pull.NextPage)
	if page.Failure != nil {
		attempts := pull.Attempts + 1
		// 参数都是字符串的消息总能编码。
		lastError, _ := common.Marshal(page.Failure)
		updates := map[string]any{"status": model.PayablePullFailed, "attempts": attempts, "last_error": string(lastError),
			"next_retry_at": int64(0), "finished_time": now}
		if page.Retryable {
			updates["status"] = model.PayablePullRetrying
			updates["next_retry_at"] = now + int64(syncRetryDelay(attempts, page.RetryAfter)/time.Second)
			delete(updates, "finished_time")
		}
		_, err := model.SavePayablePullAttempt(pull.Id, lease, nil, updates, now)
		return false, err
	}

	rows, err := newPayableUsageRows(pull, page.Items, now)
	if err != nil {
		return false, err
	}
	status := model.PayablePullPending
	updates := map[string]any{"attempts": 0, "last_error": "", "reported_total": page.Total, "next_retry_at": int64(0)}
	endOfPass := len(page.Items) == 0 || pull.NextPage*channelPushListPageSize >= page.Total
	count := pull.ChannelCount + len(rows)
	switch {
	case !endOfPass && pull.NextPage < channelPushMaxListPages:
		updates["next_page"] = pull.NextPage + 1
	case !endOfPass:
		status = model.PayablePullFailed
		lastError, _ := common.Marshal(common.NewMessage("Site channel list exceeds the page limit"))
		updates["last_error"] = string(lastError)
		updates["finished_time"] = now
	case count >= page.Total:
		status = model.PayablePullSuccess
		updates["finished_time"] = now
	case pull.Pass == 1:
		// 拉取期间渠道增删会让按 ID 翻页漏掉渠道：从第 1 页补扫，已有明细的渠道跳过。
		updates["pass"], updates["next_page"] = 2, 1
	default:
		status = model.PayablePullSuccess
		note, _ := common.Marshal(common.NewMessage("Site reported {{total}} channels but {{count}} were pulled",
			map[string]any{"total": page.Total, "count": count}))
		updates["note"] = string(note)
		updates["finished_time"] = now
	}
	updates["status"] = status
	saved, err := model.SavePayablePullAttempt(pull.Id, lease, rows, updates, now)
	return saved && status == model.PayablePullPending, err
}

// newPayableUsageRows 为本页里该进度还没有明细的渠道生成明细行。增量以该站点上一次地址相同的完整拉取为基线：
// 没有基线时为空（首次同步）；基线里没有的渠道是新渠道，基线为 0，但基线那次拉取少于站点报的总数时，
// 这样的渠道可能只是那次漏拉了，同样按首次同步；累计值比基线小时记 0（累计值回退）。
// 提供商按手动指定、渠道名标记、历史明细的顺序确定（每页批量解析一次），并快照该用户此刻的应付折扣。
func newPayableUsageRows(pull model.PayablePullSite, items []ChannelPushSiteChannel, now int64) ([]*model.PayableChannelUsage, error) {
	if len(items) == 0 {
		return nil, nil
	}
	ids := make([]int, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	existing, err := model.ExistingPayableChannels(pull.Id, ids)
	if err != nil {
		return nil, err
	}
	fresh := make([]ChannelPushSiteChannel, 0, len(items))
	freshIDs := make([]int, 0, len(items))
	for _, item := range items {
		if existing[item.ID] {
			continue
		}
		existing[item.ID] = true // 同一页里重复的渠道只写一次
		fresh = append(fresh, item)
		freshIDs = append(freshIDs, item.ID)
	}
	if len(fresh) == 0 {
		return nil, nil
	}
	found, complete, baseline, err := model.PayableBaseline(pull.SiteId, pull.BillDate, pull.SiteBaseURL, freshIDs)
	if err != nil {
		return nil, err
	}

	remoteChannels := make([]model.PayableRemoteChannel, len(fresh))
	for i, item := range fresh {
		remoteChannels[i].ID = item.ID
		if markerSiteID, localID, ok := ParseChannelPushMarker(item.Name); ok && markerSiteID == pull.SiteId {
			remoteChannels[i].LocalChannelID = localID
		}
	}
	providers, err := model.ResolvePayableProviders(pull.SiteId, pull.BillDate, remoteChannels)
	if err != nil {
		return nil, err
	}

	syncedAt := time.Now().Unix()
	rows := make([]*model.PayableChannelUsage, 0, len(fresh))
	for _, item := range fresh {
		provider := providers[item.ID]
		row := &model.PayableChannelUsage{
			TaskId:            pull.TaskId,
			PullSiteId:        pull.Id,
			BillDate:          pull.BillDate,
			SiteId:            pull.SiteId,
			SiteName:          pull.SiteName,
			SiteBaseURL:       pull.SiteBaseURL,
			RemoteChannelId:   item.ID,
			RemoteChannelName: truncateText(item.Name, payableChannelNameSize),
			UsedQuota:         item.UsedQuota,
			QuotaPerUnit:      model.ReceivableBillQuotaPerUnit,
			ProviderId:        provider.ID,
			ProviderName:      provider.Name,
			ProviderSource:    provider.Source,
			PayableDiscount:   provider.Discount,
			SyncedAt:          syncedAt,
			CreatedTime:       now,
			UpdatedTime:       now,
		}
		base, inBaseline := baseline[item.ID]
		delta := item.UsedQuota - base // 不在基线里时 base 为 0
		switch {
		case !found, !inBaseline && !complete:
			// 没有基线；或基线那次拉取少于站点报的总数，基线里没有这个渠道不能说明它是新建的
			row.DeltaFlag = model.PayableDeltaFirstSync
		case !inBaseline:
			row.DeltaQuota, row.DeltaFlag = &delta, model.PayableDeltaNewChannel
		case delta >= 0:
			row.DeltaQuota, row.DeltaFlag = &delta, model.PayableDeltaNormal
		default:
			delta = 0
			row.DeltaQuota, row.DeltaFlag = &delta, model.PayableDeltaRollback
		}
		rows = append(rows, row)
	}
	return rows, nil
}

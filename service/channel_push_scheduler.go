package service

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	channelPushScanInterval  = 15 * time.Second
	channelPushMaxConcurrent = 4
)

var (
	channelPushSchedulerOnce sync.Once
	channelPushRunningTasks  sync.Map
	channelPushSlots         = make(chan struct{}, channelPushMaxConcurrent)
)

// StartChannelPushScheduler recovers runs a restart interrupted, then scans for
// due push tasks every 15 seconds. It runs on the master node only.
func StartChannelPushScheduler() {
	channelPushSchedulerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		ChannelPushDefaultOptions.MaxRetries = ChannelPushMaxRetriesFromEnv()
		recovered, err := model.RecoverInterruptedChannelPushRuns(common.GetTimestamp())
		if err != nil {
			common.SysError(common.LogText("channel push recovery failed: %v", err))
		} else if recovered > 0 {
			common.SysLog(common.LogText("channel push runs recovered after restart: count=%d", recovered))
		}
		gopool.Go(func() {
			ticker := time.NewTicker(channelPushScanInterval)
			defer ticker.Stop()
			scanChannelPushTasks()
			for range ticker.C {
				scanChannelPushTasks()
			}
		})
	})
}

// scanChannelPushTasks claims the due tasks and runs each in its own goroutine,
// at most channelPushMaxConcurrent at a time; the rest wait for the next scan.
func scanChannelPushTasks() {
	tasks, err := model.ListDueChannelPushTasks(common.GetTimestamp(), channelPushMaxConcurrent*4)
	if err != nil {
		common.SysError(common.LogText("channel push scan failed: %v", err))
		return
	}
	for _, task := range tasks {
		if _, running := channelPushRunningTasks.LoadOrStore(task.Id, struct{}{}); running {
			continue
		}
		select {
		case channelPushSlots <- struct{}{}:
		default:
			channelPushRunningTasks.Delete(task.Id)
			return
		}
		run, ok, err := model.ClaimChannelPushRun(task, common.GetTimestamp())
		if err != nil || !ok {
			if err != nil {
				common.SysError(common.LogText("channel push claim failed: task=%d error=%v", task.Id, err))
			}
			<-channelPushSlots
			channelPushRunningTasks.Delete(task.Id)
			continue
		}
		gopool.Go(func() {
			defer func() {
				<-channelPushSlots
				channelPushRunningTasks.Delete(task.Id)
			}()
			ExecuteChannelPushRun(context.Background(), run, ChannelPushDefaultOptions)
			common.SysLog(common.LogText("channel push run finished: task=%d run=%d status=%s", task.Id, run.Id, run.Status))
		})
	}
}

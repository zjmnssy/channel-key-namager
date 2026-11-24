package service

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/bytedance/gopkg/util/gopool"
)

const payableBillScanInterval = time.Minute

var payableBillSchedulerOnce sync.Once

// StartPayableBillScheduler 只在主节点运行：启动时立即扫描一次，之后每分钟一次。
// 扫描在 ticker 循环里执行，上一次扫描结束前不会开始下一次。
func StartPayableBillScheduler() {
	payableBillSchedulerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(payableBillScanInterval)
			defer ticker.Stop()
			RunPayableBillScan(context.Background(), time.Now())
			for range ticker.C {
				RunPayableBillScan(context.Background(), time.Now())
			}
		})
	})
}

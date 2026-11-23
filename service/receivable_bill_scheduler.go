package service

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/bytedance/gopkg/util/gopool"
)

const receivableBillScanInterval = time.Minute

var receivableBillSchedulerOnce sync.Once

// StartReceivableBillScheduler scans for missing and due receivable bill rows right
// away and then every minute, on the master node only. A scan runs inside the
// ticker loop, so the next one never starts before the previous one has finished.
func StartReceivableBillScheduler() {
	receivableBillSchedulerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(receivableBillScanInterval)
			defer ticker.Stop()
			RunReceivableBillScan(context.Background(), time.Now())
			for range ticker.C {
				RunReceivableBillScan(context.Background(), time.Now())
			}
		})
	})
}

package hutils

import (
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"time"
)

type gcFinalizer struct {
	ref *gcFinalizerRef
}

type gcFinalizerRef struct {
	parent *gcFinalizer
}

// GcMemoryLimit GC 触发优化器
// 优化方式为：
// 1. 关闭自动 GC，设置初始化 GC Memory Limit
// 2. GC 触发时，增加 step 的内存限制，在连续 GC 触发时，翻倍增加内存限制
// 3. 每隔 d 时间，减少一个 step 大小的内存限制，避免内存持续在高位
type GcMemoryLimit struct {
	curr  int64
	min   int64
	max   int64
	step  int64
	multi int64

	td     time.Duration
	gd     time.Duration
	lastGc time.Time

	fGc    func(int64)
	fTimer func(int64)
}

// NewGcMemoryLimit 创建 GC 内存限制优化器
// min: 最小内存限制
// max: 最大内存限制
// step: 每次变更增加或减小的内存限制
// d: 每隔 d 时间，减少一个 step 大小的内存限制
// 例如 NewGcMemoryLimit(100*1024*1024, 1*1024*1024*1024, 100*1024*1024, time.Minute*30)
func NewGcMemoryLimit(min, max, step int64, d time.Duration) *GcMemoryLimit {
	return &GcMemoryLimit{min: min, max: max, step: step, td: d, gd: time.Minute, curr: min, multi: 1}
}

func (gml *GcMemoryLimit) SetGcHandler(fGc func(int64)) {
	gml.fGc = fGc
}

func (gml *GcMemoryLimit) SetTimerHandler(fTimer func(int64)) {
	gml.fTimer = fTimer
}

func (gml *GcMemoryLimit) Run() {
	gml.initFinalizer()
	debug.SetGCPercent(-1)
	debug.SetMemoryLimit(gml.min)
	go gml.run()
}

func (gml *GcMemoryLimit) initFinalizer() {
	f := &gcFinalizer{}
	f.ref = &gcFinalizerRef{parent: f}
	runtime.SetFinalizer(f.ref, gml.finalizerHandler)
	f.ref = nil
}

func (gml *GcMemoryLimit) finalizerHandler(ref *gcFinalizerRef) {
	// 距离上一次 gc 超过 1min，则重置增长倍率
	if time.Now().Sub(gml.lastGc) > gml.gd {
		gml.multi = 1
	}
	gml.lastGc = time.Now()
	gml.lastGc = time.Now()
	curr := atomic.AddInt64(&gml.curr, gml.step*gml.multi)
	curr = Clamp(curr, gml.min, gml.max)
	debug.SetMemoryLimit(curr)
	if gml.fGc != nil {
		gml.fGc(curr)
	}

	gml.multi *= 2

	runtime.SetFinalizer(ref, gml.finalizerHandler)
}

func (gml *GcMemoryLimit) run() {
	for {
		// 每隔 d 时间，减少一个 step 大小的内存限制，避免内存持续在高位
		time.Sleep(gml.td)
		gml.multi = 1
		curr := atomic.AddInt64(&gml.curr, -gml.step)
		curr = Clamp(curr, gml.min, gml.max)
		debug.SetMemoryLimit(curr)
		if gml.fTimer != nil {
			gml.fTimer(curr)
		}
	}
}

package bbr

import (
	"github.com/godaner/logger"
	"math"
	"sync"
	"time"
)

const (
	maxCongWinSize = 32
	minCongWinSize = 1
	startUpWinSize = 1
	probRTTWinSize = 4
)
const (
	statusStartUp = iota
	statusDrain
	statusProbBW
	statusProbRTT
)

const (
	bbrStartUpGain = 2
	bbrDrainGain   = 2
	bbrProbRTTGain = 3
	bbrProbBWGain  = 1.25
)
const (
	unit = float64(1)
)
const (
	minRttWin = time.Duration(10) * time.Second
	minMaxWin = time.Duration(10) * time.Second
	//dRateWin  = time.Duration(1) * time.Second
	dRateWin = 10
)
const (
	probRTTTime = time.Duration(200) * time.Millisecond
)
const (
	maxFullBwCnt = 3
)
const (
	fullBwThresh = float64(1.25)
)

var bbrPacingGain = []float64{
	unit * 5.0 / 4.0, /* probe for more available bw */
	unit * 3.0 / 4.0, /* drain queue and/or yield bw to other flows */
	unit, unit, unit, /* cruise at 1.0*bw to utilize pipe, */
	unit, unit, unit, /* without creating excess queue... */
}

func init() {
	//if defCongWinSize > defRecWinSize {
	//	panic("defCongWinSize > defRecWinSize")
	//}
	//if defCongWinSize > defRecWinSize {
	//	panic("defCongWinSize > defRecWinSize")
	//}
	//if defCongWinSize <= 0 {
	//	panic("defCongWinSize <= 0")
	//}
	if minCongWinSize > maxCongWinSize {
		panic("minCongWinSize > maxCongWinSize")
	}
}

// BBR
type BBR struct {
	sync.Once
	minRttUs                 time.Duration /* min RTT in min_rtt_win_sec window */
	minRttStamp              time.Time     /* timestamp of min_rtt_us */
	probeRttDoneStamp        time.Time     /* end time for BBR_PROBE_RTT mode */
	bw                       minMax        /* Max recent delivery rate in pkts/uS << 24 */
	fullBwCnt                uint8
	fullBw                   float64
	sts                      int
	cwnd                     int64
	bdp                      int64
	cPacing                  uint8
	drateSStamp, drateEStamp time.Time
	drate                    float64
	drateD                   uint16
	probBWMaxBDP             int64
	probBWMinRTT             time.Duration
	//probRTTStart             bool
	Logger logger.Logger
}

func (b *BBR) init() {
	b.Do(func() {
		b.setStatusStartUp()
		b.bw = minMax{
			Win: minMaxWin,
		}
		//b.setCWND(defCongWinSize)
	})
}

// setMinRttUs
func (b *BBR) setMinRttUs(rttus time.Duration) (expired bool) {
	now := time.Now()
	expired = b.isMinRttExpired(now)
	if rttus >= 0 && (rttus <= b.minRttUs || expired || b.minRttStamp.IsZero()) {
		b.minRttUs = rttus
		b.minRttStamp = now
	}
	return expired
}

// isMinRttExpired
func (b *BBR) isMinRttExpired(now time.Time) (expired bool) {
	return (!b.minRttStamp.IsZero()) && now.After(b.minRttStamp.Add(minRttWin))
}

// setBw
func (b *BBR) setBw(bw float64) {
	b.bw.updateMax(time.Now(), bw)
}

// getMaxBW
func (b *BBR) getMaxBW() (mbw float64) {
	return b.bw.s[0].v
}

// fullBwReached
func (b *BBR) fullBwReached() (reached bool) {
	return b.fullBwCnt >= maxFullBwCnt
}

// resetFullBw
func (b *BBR) resetFullBw() {
	b.fullBwCnt = 0
	b.fullBw = 0
}

// checkFullBwReached
func (b *BBR) checkFullBwReached() {

	// 这里是一个简单的冒泡法，只要不是连续的带宽增长小于25%，那么就将计数“不增长阈值”加1，事不过三，超过三次，切换到DRAIN
	bwThresh := b.fullBw * fullBwThresh
	fb := b.fullBw
	maxBw := b.getMaxBW()
	defer func() {
		b.Logger.Critical("checkFullBwReached : fullBw is", fb, "maxBw is", maxBw, "bwThresh is", bwThresh, "fullBwCnt is", b.fullBwCnt)
	}()
	if b.fullBwReached() {
		b.Logger.Critical("checkFullBwReached : fullBwReached !!!!!!!")
		return
	}
	if maxBw >= bwThresh {
		b.fullBw = maxBw
		b.fullBwCnt = 0
		return
	}
	b.fullBwCnt++
}

// comDR
//func (b *BBR) comDR(rttus time.Duration) (bw float64) {
//	return float64(1)/float64(rttus/time.Millisecond)
//}

func (b *BBR) markBDP() {
	b.bdp = int64(math.Ceil((float64(b.minRttUs) / float64(time.Millisecond)) * b.getMaxBW()))
}

// bdp
func (b *BBR) getBdp() (bdp int64) {
	//if b.bdp <= 0 {
	//	return 1
	//}
	return b.bdp
}
func (b *BBR) GetCWND() (cwnd int64) {
	b.init()
	return b.cwnd
}

// setDRate
func (b *BBR) setDRate(s, e time.Time) (bw float64) {
	//b.Logger.Critical("setDRate : drate is", b.drate, "drateD is", b.drateD, "drateSStamp is", b.drateSStamp, "drateEStamp is", b.drateEStamp, "s is", s, "e is", e)
	if b.drateD >= dRateWin {
		b.drateD = 0
	}
	if b.drateD == 0 {
		b.drateSStamp = s
		b.drateEStamp = e
		b.drate = 0
	}
	if s.Before(b.drateSStamp) {
		b.drateSStamp = s
	}
	if e.After(b.drateEStamp) {
		b.drateEStamp = e
	}
	b.drateD++
	b.drate = float64(b.drateD) / (float64(b.drateEStamp.Sub(b.drateSStamp)) / float64(time.Millisecond))
	return b.drate
	//if now.After(b.drateStamp.Add(dRateWin)) { // expired
	//	b.drate =
	//	b.drateD = 0
	//	b.drateStamp = now
	//	c = true
	//}
	//b.drateD++
	//return c
}

// getDRate
func (b *BBR) getDRate() (c float64) {
	return b.drate
}

// Update
func (b *BBR) Update(inflight int64, rttus time.Duration, s, e time.Time) {
	b.init()
	switch b.sts {
	case statusStartUp:
		bw := b.setDRate(s, e)
		obw := b.getMaxBW()
		b.setBw(bw)
		expired := b.setMinRttUs(rttus)
		if expired { //cong
			b.Logger.Critical("setStatusStartUp : end , into setStatusProbRTT ")
			b.setStatusProbRTT()
			return
		}
		b.Logger.Critical("setStatusStartUp : bw is", bw, "maxbw is", b.getMaxBW(), "fullbwcnt is", b.fullBwCnt, "rtt is", rttus, "minrtt is", b.minRttUs)
		b.checkFullBwReached()
		if b.fullBwReached() {
			b.markBDP()
			b.Logger.Critical("setStatusStartUp : end , into setStatusDrain , dbp is", b.getBdp(), "maxxbw is", b.getMaxBW(), "minrtt is", b.minRttUs)
			b.setStatusDrain()
			return
		}
		if b.getMaxBW() >= obw {
			b.setCWND(b.cwnd * bbrStartUpGain)
		}
		b.Logger.Critical("setStatusStartUp : cwnd is", b.cwnd)
		return
	case statusDrain:
		b.Logger.Critical("setStatusDrain : inflight is", inflight, "bdp is", b.getBdp())
		bw := b.setDRate(s, e)
		b.setBw(bw)
		expired := b.setMinRttUs(rttus)
		//b.markBDP()
		if expired { //cong
			b.Logger.Critical("setStatusDrain : end , into setStatusProbRTT ")
			b.setStatusProbRTT()
			return
		}
		if inflight > b.getBdp() {
			return
		}
		b.Logger.Critical("setStatusDrain : end , into setStatusProbBW , bdp is ", b.getBdp())
		b.setStatusProbBW()
	case statusProbBW:
		bw := b.setDRate(s, e)
		b.setBw(bw)
		expired := b.setMinRttUs(rttus)
		b.markBDP()
		if expired { //cong
			b.Logger.Critical("setStatusProbBW : end , into setStatusProbRTT ")
			b.setStatusProbRTT()
			return
		}
		if b.probBWMinRTT == 0 {
			b.probBWMinRTT = b.minRttUs
		}
		bdp := b.getBdp()
		if bdp > b.probBWMaxBDP && b.minRttUs <= b.probBWMinRTT {
			b.probBWMaxBDP = bdp
			b.probBWMinRTT = b.minRttUs
		}
		//b.setCWND(b.probBWMaxBDP)
		//pacing
		if b.cPacing >= uint8(len(bbrPacingGain)) {
			b.cPacing = uint8(0)
		}
		//b.setCWND(int64(math.Ceil(float64(b.probBWMaxBDP) * bbrProbBWGain)))
		b.setCWND(int64(math.Ceil(float64(b.probBWMaxBDP) * bbrPacingGain[b.cPacing])))
		b.cPacing++
		b.Logger.Critical("setStatusProbBW : , b.cwnd is ", b.cwnd)
	case statusProbRTT:
		b.setMinRttUs(rttus)
		bw := b.setDRate(s, e)
		b.setBw(bw)
		b.checkFullBwReached()
		b.Logger.Critical("setStatusProbRTT : ")
		if b.probeRttDoneStamp.After(time.Now()) {
			return
		}
		if b.fullBwReached() {
			b.markBDP() // for setStatusProbBW
			b.Logger.Critical("setStatusProbRTT : end , into setStatusProbBW ")
			b.setStatusProbBW()
		} else {
			b.Logger.Critical("setStatusProbRTT : end , into setStatusStartUp ")
			b.setStatusStartUp()
		}
		//b.setCWND(b.getBdp())
	}
}

// setStatusStartUp
func (b *BBR) setStatusStartUp() {
	b.setCWND(startUpWinSize)
	b.resetFullBw()
	b.bw.reset(time.Now(), 0)
	b.resetMinRtt()
	b.sts = statusStartUp
	b.Logger.Critical("setStatusStartUp : start")
}

// setStatusProbRTT
func (b *BBR) setStatusProbRTT() {
	b.setCWND(probRTTWinSize)
	//b.probRTTStart = false
	b.probeRttDoneStamp = time.Now().Add(probRTTTime)
	b.resetFullBw()
	b.bw.reset(time.Now(), 0)
	b.resetMinRtt()
	b.sts = statusProbRTT
	b.Logger.Critical("setStatusProbRTT : start")
}

// setStatusDrain
func (b *BBR) setStatusDrain() {
	//b.setCWND(bbrDrainGain)
	b.setCWND(b.getBdp())
	b.resetMinRtt()
	b.sts = statusDrain
	b.Logger.Critical("setStatusDrain : start")
}

// setStatusProbBW
func (b *BBR) setStatusProbBW() {
	bdp := b.getBdp()
	b.probBWMaxBDP = bdp
	b.probBWMinRTT = 0
	b.setCWND(b.probBWMaxBDP)
	// todo reset minrtts ??
	b.resetMinRtt()
	b.sts = statusProbBW
	b.Logger.Critical("setStatusProbBW : start")
}

// setCWND
func (b *BBR) setCWND(n int64) (c int64) {
	b.cwnd = n
	b.cwnd = _maxi64(b.cwnd, minCongWinSize)
	b.cwnd = _mini64(b.cwnd, maxCongWinSize)
	return b.cwnd
}

func (b *BBR) resetMinRtt() {
	b.minRttUs = 0
	b.minRttStamp = time.Time{}
}
func _maxi64(a, b int64) (c int64) {
	if a > b {
		return a

	}
	return b
}
func _mini64(a, b int64) (c int64) {
	if a > b {
		return b
	}
	return a
}

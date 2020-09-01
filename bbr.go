package geronimo

import (
	"github.com/godaner/logger"
	"math"
	"sync"
	"time"
)

const (
	defRecWinSize  = 512
	maxCongWinSize = 512
	minCongWinSize = 1
	startUpWinSize = 4
	probRTTWinSize = 4
)
const (
	statusStartUp = iota
	statusDrain
	statusProbBW
	statusProbRTT
)

const (
	bbrStartUpGain = 3
	//bbrDrainGain   = 2
	//bbrProbRTTGain = 3
	//bbrProbBWGain  = 1.25
)
const (
	unit = float64(1)
)
const (
	minRttWin = time.Duration(10) * time.Second
	minMaxWin = time.Duration(10) * time.Second
	//dRateWin  = time.Duration(1) * time.Second
	//dRateWin = 10
)
const (
	probRTTTime = time.Duration(200) * time.Millisecond
)
const (
	maxFullBwCnt = 3
)
const (
	fullBwThresh = float64(1.1)
)

var bbrPacingGain = []float64{
	unit * 5.0 / 4.0, /* probe for more available bw */
	//unit * 8.0 / 4.0, /* probe for more available bw */
	//unit * 3.0 / 4.0, /* drain queue and/or yield bw to other flows */
	//unit * 1.0 / 2.0, /* drain queue and/or yield bw to other flows */
	unit, unit, unit, /* cruise at 1.0*bw to utilize pipe, */
	unit, unit, unit, /* without creating excess queue... */
}

func init() {
	//if defCongWinSize > defRecWinSize {
	//	panic("defCongWinSize > defRecWinSize")
	//}4
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
	minRttUs          time.Duration /* min RTT in min_rtt_win_sec window */
	minRttStamp       time.Time     /* timestamp of min_rtt_us */
	probeRttDoneStamp time.Time     /* end time for BBR_PROBE_RTT mode */
	bw                minMax        /* Max recent delivery rate in pkts/uS << 24 */
	fullBwCnt         uint8
	fullBw            float64
	sts               int
	cwnd              int64
	cPacing           uint8
	probBWMaxBDP      int64
	probBWMinRTT      time.Duration
	delivered         int64
	nextRttDelivered  int64
	deliveredTime     time.Time
	roundStart        bool
	Logger            logger.Logger
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

//func (b *BBR) minRtt() (minRTT time.Duration) {
//	return b.minRttUs
//}
// setMinRtt
func (b *BBR) setMinRtt(rttus time.Duration) (expired bool) {
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

// updateBw
func (b *BBR) updateBw(bw float64) {
	b.bw.updateMax(time.Now(), bw)
}

// maxBW
func (b *BBR) maxBW() (mbw float64) {
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
	//fb := b.fullBw
	maxBw := b.maxBW()
	defer func() {
		//b.Logger.Critical("checkFullBwReached : fullBw is", fb, "maxBw is", maxBw, "bwThresh is", bwThresh, "fullBwCnt is", b.fullBwCnt)
	}()
	if b.fullBwReached() {
		//b.Logger.Critical("checkFullBwReached : fullBwReached !!!!!!!")
		return
	}
	if !b.roundStart {
		return
	}
	if maxBw >= bwThresh {
		b.fullBw = maxBw
		b.fullBwCnt = 0
		return
	}
	b.fullBwCnt++
}

func (b *BBR) comBDP(minRtts time.Duration, maxBw float64) (bdp int64) {
	return int64(math.Ceil((float64(minRtts) / float64(time.Millisecond)) * maxBw))
}

func (b *BBR) CWND() (cwnd int64) {
	b.init()
	return b.cwnd
}

// Update
func (b *BBR) Update(inflight int64, seg *Segment) {
	b.init()
	// delivered
	b.delivered++
	// check round start
	b.checkRoundStart(seg)
	// old
	oBW := b.maxBW()
	oMinRTT := b.minRttUs
	// update max bw and min rtt
	dr := b.deliveryRate(seg)
	b.updateBw(dr)
	expired := b.setMinRtt(seg.RTT())
	//b.Logger.Critical("Update : rtt is ", seg.RTT())
	switch b.sts {
	case statusStartUp:
		if expired { //cong
			//b.Logger.Critical("setStatusStartUp : end , into setStatusProbRTT ")
			b.setStatusProbRTT()
			return
		}
		//b.Logger.Critical("setStatusStartUp : dr is", dr, "maxbw is", b.maxBW(), "fullbwcnt is", b.fullBwCnt, "rtt is", seg.RTT(), "minrtt is", b.minRttUs)
		b.checkFullBwReached()
		if b.fullBwReached() {
			//b.markBDP()
			//b.Logger.Critical("setStatusStartUp : end , into setStatusDrain , dbp is", b.comBDP(b.minRttUs, b.maxBW()), "maxxbw is", b.maxBW(), "minrtt is", b.minRttUs)
			b.setStatusDrain()
			return
		}
		if b.maxBW() >= oBW*fullBwThresh {
			b.setCWND(b.cwnd * bbrStartUpGain)
		}
		//b.Logger.Critical("setStatusStartUp : cwnd is", b.cwnd)
		return
	case statusDrain:
		b.setCWND(b.comBDP(b.minRttUs, b.maxBW()))
		//b.Logger.Critical("setStatusDrain : inflight is", inflight, "inbdp is", b.comBDP(oMinRTT, b.maxBW()), "cwnd bdp is", b.comBDP(b.minRttUs, b.maxBW()))
		if expired { //cong
			//b.Logger.Critical("setStatusDrain : end , into setStatusProbRTT ")
			b.setStatusProbRTT()
			return
		}
		if inflight > b.comBDP(oMinRTT, b.maxBW()) {
			return
		}
		//b.Logger.Critical("setStatusDrain : end , into setStatusProbBW , bdp is ", b.comBDP(b.minRttUs, b.maxBW()))
		b.setStatusProbBW()
	case statusProbBW:
		if expired { //cong
			//b.Logger.Critical("setStatusProbBW : end , into setStatusProbRTT ")
			b.setStatusProbRTT()
			return
		}
		bdp := b.comBDP(b.minRttUs, b.maxBW())
		if b.probBWMaxBDP == 0 {
			b.probBWMaxBDP = bdp
		}
		if b.probBWMinRTT == 0 {
			b.probBWMinRTT = b.minRttUs
		}
		if bdp > b.probBWMaxBDP && b.minRttUs <= b.probBWMinRTT {
			b.probBWMaxBDP = bdp
			b.probBWMinRTT = b.minRttUs
		}
		//b.setCWND(b.probBWMaxBDP*2)
		//pacing
		if b.cPacing >= uint8(len(bbrPacingGain)) {
			b.cPacing = uint8(0)
		}
		b.setCWND(int64(math.Ceil(float64(b.probBWMaxBDP) * bbrPacingGain[b.cPacing])))
		b.cPacing++
		//b.Logger.Critical("setStatusProbBW : , b.cwnd is ", b.cwnd)
	case statusProbRTT:
		b.checkFullBwReached()
		//b.Logger.Critical("setStatusProbRTT : ")
		if !b.roundStart{
			return
		}
		//if b.probeRttDoneStamp.After(time.Now()) {
		//	return
		//}
		if b.fullBwReached() {
			//b.markBDP() // for setStatusProbBW
			//b.Logger.Critical("setStatusProbRTT : end , into setStatusProbBW ")
			b.setStatusProbBW()
		} else {
			//b.Logger.Critical("setStatusProbRTT : end , into setStatusStartUp ")
			b.setStatusStartUp()
		}
		//b.setCWND(b.getBdp())
	}
}

// setStatusStartUp
func (b *BBR) setStatusStartUp() {
	b.setCWND(startUpWinSize)
	b.resetFullBw()
	// b.bw.reset(time.Now(), 0)
	//b.resetMinRtt()
	b.sts = statusStartUp
	//b.Logger.Critical("setStatusStartUp : start")
}

// setStatusDrain
func (b *BBR) setStatusDrain() {
	//b.setCWND(bbrDrainGain)
	//b.setCWND(b.getBdp())
	//b.resetMinRtt()
	b.sts = statusDrain
	//b.Logger.Critical("setStatusDrain : start")
}

// setStatusProbBW
func (b *BBR) setStatusProbBW() {
	//bdp := b.getBdp()
	b.probBWMaxBDP = 0
	b.probBWMinRTT = 0
	b.setCWND(b.probBWMaxBDP)
	// todo reset minrtts ??
	//b.resetMinRtt()
	b.sts = statusProbBW
	//b.Logger.Critical("setStatusProbBW : start")
}

// setStatusProbRTT
func (b *BBR) setStatusProbRTT() {
	b.setCWND(probRTTWinSize)
	//b.probRTTStart = false
	b.probeRttDoneStamp = time.Now().Add(probRTTTime)
	b.resetFullBw()
	// b.bw.reset(time.Now(), 0)
	//b.resetMinRtt()
	b.sts = statusProbRTT
	//b.Logger.Critical("setStatusProbRTT : start")
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

func (b *BBR) deliveryRate(seg *Segment) float64 {
	b.deliveredTime = time.Now() // last ack time
	return float64(b.delivered-seg.Delivered()) / (float64(b.deliveredTime.Sub(seg.DeliveredTime())) / float64(time.Millisecond))
	//return float64(b.delivered-seg.Delivered()) / float64(time.Now().Sub(seg.DeliveredTime()))
}

func (b *BBR) Delivered() (d int64) {
	return b.delivered
}
func (b *BBR) DeliveredTime() (dt time.Time) {
	if b.deliveredTime.IsZero() {
		b.deliveredTime = time.Now()
	}
	return b.deliveredTime
}

func (b *BBR) checkRoundStart(seg *Segment) {
	//if (!before(rs->prior_delivered, bbr->next_rtt_delivered)) {
	//	bbr->next_rtt_delivered = tp->delivered;
	//	bbr->rtt_cnt++;
	//	bbr->round_start = 1;
	//	bbr->packet_conservation = 0;
	//}
	b.roundStart = false
	if seg.delivered >= b.nextRttDelivered {
		//b.nextRttDelivered = seg.delivered
		b.nextRttDelivered = b.delivered
		b.roundStart = true
	}
}

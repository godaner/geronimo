package geronimo

import (
	"sync"
	"time"
)

type minMax struct {
	sync.Once
	s   [3]minMaxSample
	Win time.Duration
}
type minMaxSample struct {
	t time.Time /* time measurement was taken */
	v float64   /* value measured */
}

func (m *minMax) init() {
	m.Do(func() {
		m.s = [3]minMaxSample{}
	})
}
func (m *minMax) reset(t time.Time, v float64) (maxV float64) {
	val := minMaxSample{
		t: t,
		v: v,
	}
	m.s[0] = val
	m.s[1] = m.s[0]
	m.s[2] = m.s[1]
	return m.s[0].v
}

// updateMax
//  Check if new measurement updates the 1st, 2nd or 3rd choice max.
func (m *minMax) updateMax(t time.Time, v float64) (maxV float64) {
	val := minMaxSample{
		t: t,
		v: v,
	}
	if val.t.After(m.s[2].t.Add(m.Win)) { /* nothing left in window? */
		return m.reset(t, v) /* forget earlier samples */
	}
	if val.v >= m.s[0].v { /* found new max? */
		return m.reset(t, v) /* forget earlier samples */
	}

	if val.v >= m.s[1].v {
		m.s[1] = val
		m.s[2] = m.s[1]
	} else if val.v >= m.s[2].v {
		m.s[2] = val
	}
	return m.updateSubWin(&val)
}

// updateSubWin
//  As time advances, update the 1st, 2nd, and 3rd choices.
func (m *minMax) updateSubWin(val *minMaxSample) (maxV float64) {
	dt := val.t.Sub(m.s[0].t)
	if dt > m.Win {
		/*
		 * Passed entire window without a new val so make 2nd
		 * choice the new val & 3rd choice the new 2nd choice.
		 * we may have to iterate this since our 2nd choice
		 * may also be outside the window (we checked on entry
		 * that the third choice was in the window).
		 */
		m.s[0] = m.s[1]
		m.s[1] = m.s[2]
		m.s[2] = *val
		if val.t.After(m.s[0].t.Add(m.Win)) {
			m.s[0] = m.s[1]
			m.s[1] = m.s[2]
			m.s[2] = *val
		}
	} else if m.s[1].t == m.s[0].t && dt > m.Win/4 {
		/*
		 * We've passed a quarter of the window without a new val
		 * so take a 2nd choice from the 2nd quarter of the window.
		 */
		m.s[1] = *val
		m.s[2] = m.s[1]
	} else if m.s[2].t == m.s[1].t && dt > m.Win/2 {
		/*
		 * We've passed half the window without finding a new val
		 * so take a 3rd choice from the last half of the window
		 */
		m.s[2] = *val
	}
	return m.s[0].v
}

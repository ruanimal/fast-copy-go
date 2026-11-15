package copyengine

import "testing"

func TestClassifyDevices(t *testing.T) {
	hdd := device{id: "a", rotational: true}
	hdd2 := device{id: "b", rotational: true}
	ssd := device{id: "c"}
	for _, tc := range []struct {
		a, b     device
		okA, okB bool
		want     DiskMode
	}{{hdd, hdd, true, true, DiskSame}, {hdd, hdd2, true, true, DiskDifferent}, {ssd, ssd, true, true, DiskDifferent}, {hdd, hdd, false, true, DiskSame}} {
		if got := classifyDevices(tc.a, tc.b, tc.okA, tc.okB); got != tc.want {
			t.Errorf("got %s want %s", got, tc.want)
		}
	}
}

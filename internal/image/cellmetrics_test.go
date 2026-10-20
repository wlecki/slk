package image

import "testing"

func TestCellPixels_EnvOverride(t *testing.T) {
	saved := getenv
	defer func() { getenv = saved }()
	getenv = func(k string) string {
		switch k {
		case "COLORTERM_CELL_WIDTH":
			return "10"
		case "COLORTERM_CELL_HEIGHT":
			return "20"
		}
		return ""
	}

	w, h := CellPixels(0)
	if w != 10 || h != 20 {
		t.Errorf("got (%d,%d), want (10,20)", w, h)
	}
}

func TestCellPixels_FallbackWhenNoEnvAndNoFD(t *testing.T) {
	saved := getenv
	defer func() { getenv = saved }()
	getenv = func(k string) string { return "" }

	// fd = -1 forces ioctl to fail.
	w, h := CellPixels(-1)
	if w != 8 || h != 16 {
		t.Errorf("got (%d,%d), want (8,16) fallback", w, h)
	}
}

// Emulated kitty placement needs to know whether the size was measured,
// which CellPixels' (8, 16) fallback hides.
func TestMeasureCellPixels_ReportsWhetherMeasured(t *testing.T) {
	saved := getenv
	defer func() { getenv = saved }()

	getenv = func(string) string { return "" }
	if w, h, measured := MeasureCellPixels(-1); measured || w != 8 || h != 16 {
		t.Errorf("fallback: got (%d,%d,%v), want (8,16,false)", w, h, measured)
	}

	getenv = func(k string) string {
		switch k {
		case "COLORTERM_CELL_WIDTH":
			return "9"
		case "COLORTERM_CELL_HEIGHT":
			return "19"
		}
		return ""
	}
	if w, h, measured := MeasureCellPixels(-1); !measured || w != 9 || h != 19 {
		t.Errorf("env override: got (%d,%d,%v), want (9,19,true)", w, h, measured)
	}
}

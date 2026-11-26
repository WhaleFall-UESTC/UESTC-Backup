package errs

import "testing"

// 数值就是命令行的退出码，定下来就不再改，新的加在最后
func TestCodesAreExitCodes(t *testing.T) {
	tests := []struct {
		code Code
		want int
	}{
		{NotArchive, 1},
		{UnsupportedVersion, 2},
		{Corrupted, 3},
		{SourceChanged, 4},
		{IO, 5},
		{InvalidArgument, 6},
		{TargetNotEmpty, 7},
		{WrongPassword, 8},
		{PartialBackup, 9},
	}
	for _, tc := range tests {
		if int(tc.code) != tc.want {
			t.Errorf("Code(%s) = %d, want %d", tc.code, int(tc.code), tc.want)
		}
		if tc.code.String() == "" {
			t.Errorf("Code(%d) has no phrase", int(tc.code))
		}
	}
}

package convert

import (
	"testing"
)

func TestParseStrings(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    any
		wantErr bool
	}{
		{
			name:  "int slice",
			input: []string{"1", "2", "3"},
			want:  []int{1, 2, 3},
		},
		{
			name:  "int64 slice",
			input: []string{"100", "200", "300"},
			want:  []int64{100, 200, 300},
		},
		{
			name:  "float64 slice",
			input: []string{"1.5", "2.5", "3.5"},
			want:  []float64{1.5, 2.5, 3.5},
		},
		{
			name:    "invalid int",
			input:   []string{"1", "invalid", "3"},
			wantErr: true,
		},
		{
			name:  "empty slice",
			input: []string{},
			want:  []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch want := tt.want.(type) {
			case []int:
				got, err := ParseStrings[int](tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("ParseStrings() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr && !sliceEqual(got, want) {
					t.Errorf("ParseStrings() = %v, want %v", got, want)
				}
			case []int64:
				got, err := ParseStrings[int64](tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("ParseStrings() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr && !sliceEqual(got, want) {
					t.Errorf("ParseStrings() = %v, want %v", got, want)
				}
			case []float64:
				got, err := ParseStrings[float64](tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("ParseStrings() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr && !sliceEqual(got, want) {
					t.Errorf("ParseStrings() = %v, want %v", got, want)
				}
			}
		})
	}
}

func TestParseString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    any
		wantErr bool
	}{
		{name: "int", input: "42", want: 42},
		{name: "int8", input: "127", want: int8(127)},
		{name: "uint", input: "42", want: uint(42)},
		{name: "float32", input: "3.14", want: float32(3.14)},
		{name: "float64", input: "3.14159", want: 3.14159},
		{name: "invalid", input: "invalid", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch want := tt.want.(type) {
			case int:
				got, err := ParseString[int](tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("ParseString() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr && got != want {
					t.Errorf("ParseString() = %v, want %v", got, want)
				}
			case int8:
				got, err := ParseString[int8](tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("ParseString() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr && got != want {
					t.Errorf("ParseString() = %v, want %v", got, want)
				}
			case uint:
				got, err := ParseString[uint](tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("ParseString() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr && got != want {
					t.Errorf("ParseString() = %v, want %v", got, want)
				}
			case float32:
				got, err := ParseString[float32](tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("ParseString() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr && !floatEqual(float64(got), float64(want)) {
					t.Errorf("ParseString() = %v, want %v", got, want)
				}
			case float64:
				got, err := ParseString[float64](tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("ParseString() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !tt.wantErr && got != want {
					t.Errorf("ParseString() = %v, want %v", got, want)
				}
			}
		})
	}
}

// Helper functions
func sliceEqual[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func floatEqual(a, b float64) bool {
	const epsilon = 1e-6
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}

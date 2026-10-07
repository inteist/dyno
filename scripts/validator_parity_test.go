package scripts_test

import (
	"fmt"
	"math"
	"math/big"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode"
)

func intervalNanoseconds(text string) (string, error) {
	cmd := exec.Command("/usr/bin/awk", "-v", "emit_total=1", "-f", "validate-interval.awk")
	cmd.Env = []string{"LC_ALL=C", "DYNO_CHECK_INTERVAL=" + text}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestIntervalValidatorMatchesGoRoundingAndUnits(t *testing.T) {
	cases := []string{".5s", "1500µs", "1500μs", "1.5ms", "2562047.78801521550222222222h", "0.00000000000000000000000000000000000000001h", "0." + strings.Repeat("0", 350) + "1h"}
	for _, unit := range []struct {
		suffix string
		nanos  int64
	}{{"h", int64(time.Hour)}, {"m", int64(time.Minute)}, {"s", int64(time.Second)}} {
		for offset := -32; offset <= 32; offset++ {
			nanos := new(big.Int).Add(big.NewInt(math.MaxInt64), big.NewInt(int64(offset)))
			text := new(big.Rat).SetFrac(nanos, big.NewInt(unit.nanos)).FloatString(24) + unit.suffix
			cases = append(cases, text)
		}
	}
	for _, text := range cases {
		want, wantErr := time.ParseDuration(text)
		got, err := intervalNanoseconds(text)
		if (err != nil) != (wantErr != nil) {
			t.Errorf("%s: awk %q %v; Go %v", text, got, err, wantErr)
			continue
		}
		if err == nil && got != fmt.Sprint(int64(want)) {
			t.Errorf("%s: awk %s ns; Go %d ns", text, got, want)
		}
	}
}

func labelCount(text string) (string, error) {
	cmd := exec.Command("/usr/bin/awk", "-f", "validate-label.awk", "printable-ranges.txt")
	cmd.Env = []string{"LC_ALL=C", "DYNO_CHECK_TEXT=" + text}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestLabelValidatorRejectsOverlongUTF8(t *testing.T) {
	// The latter encodings pass the lead-byte check, exercising the decoded
	// codepoint minimum rather than accidentally relying only on byte ranges.
	for _, bytes := range [][]byte{{0xc1, 0x81}, {0xe0, 0x81, 0x81}, {0xf0, 0x80, 0x81, 0x81}, {0xed, 0xa0, 0x80}, {0xf4, 0x90, 0x80, 0x80}, {0xc2}} {
		if out, err := labelCount(string(bytes)); err == nil {
			t.Errorf("accepted malformed UTF-8 %x: %s", bytes, out)
		}
	}
}

func TestLabelValidatorSearchesRangeBoundaries(t *testing.T) {
	ranges := installerPrintableRanges(t)
	var printable strings.Builder
	count := 0
	for _, r := range ranges {
		printable.WriteRune(rune(r[0]))
		printable.WriteRune(rune(r[1]))
		count += 2
	}
	if out, err := labelCount(printable.String()); err != nil || out != fmt.Sprint(count) {
		t.Fatalf("all range endpoints: %q %v; want %d", out, err, count)
	}
	// Gaps exercise both sides of binary search; sample the start, middle, end,
	// and Unicode's printable/unprintable transitions without thousands of execs.
	for _, cp := range []rune{31, 32, 126, 127, 160, 161, 173, 174, 0x0377, 0x0378, 0x200a, 0x200b, 0x202e, 0x202f, 0xe000, 0x10ffff} {
		_, err := labelCount(string(cp))
		if (err == nil) != unicode.IsPrint(cp) {
			t.Errorf("U+%04X printable=%t: %v", cp, unicode.IsPrint(cp), err)
		}
	}
}

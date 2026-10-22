package qr

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// Drawn by python-qrcode 8 for the same payload, version 3, level M and mask 5.
const linkV3Mask5 = `#######...#.####.##...#######
#.....#.##....#.#.....#.....#
#.###.#.###.#.......#.#.###.#
#.###.#.####.##.#####.#.###.#
#.###.#....#.#.######.#.###.#
#.....#..##...#..#....#.....#
#######.#.#.#.#.#.#.#.#######
........###...##.............
#.....#.#.###.##.#..###..###.
###.##..##....#....#...##.##.
##.##.#..#.##.####..##..#....
.##.#..#.##.#.#...#.##.###...
##....###....#.##...#.#.....#
#..##...#.#.....#########..##
#.#.#####....####.#.##...##..
.##.....#.#.#.###...##.#..#.#
###...###...#.##.#.......##..
##...#.##.###..#.########.###
###.#.#.####...##..###..##..#
#...#..#.####.##..#..#..#....
#.....#.##..#.####.######.###
........##.#.###..###...##...
#######...##...######.#.###..
#.....#............##...#...#
#.###.#...#..####..#######.#.
#.###.#..##.#..##..#.....##.#
#.###.#....##.##...#.#######.
#.....#..............#...##.#
#######.#...##...#.#.####.#..
`

func TestALinkURLMatchesAReferenceEncoderModuleForModule(t *testing.T) {
	code, err := encode([]byte("https://fantasy.example/vincular#ABCD-EFGH"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if code.Version != 3 || code.String() != linkV3Mask5 {
		t.Errorf("version %d:\n%s", code.Version, code)
	}
}

func TestLongerPayloadsMatchAReferenceEncoder(t *testing.T) {
	for _, c := range []struct {
		length, version, mask int
		sha                   string
	}{
		{120, 7, 3, "77068da265ed0062b3035b31620d73c20c347400e791c5c4b907cbfc546f2706"},
		{160, 9, 1, "1974a867cd14d6c8ba5a974f96f2317638dc0b60623ebe9592ee97eb20317523"},
		{200, 10, 6, "3babaef6cb6db6f9a6607dd11c74905f55cdb9bce957c00fab7480607b0407ef"},
	} {
		code, err := encode([]byte(strings.Repeat("x", c.length)), c.mask)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(code.String()))
		if code.Version != c.version || hex.EncodeToString(sum[:]) != c.sha {
			t.Errorf("%d bytes: version %d, matrix differs from the reference", c.length, code.Version)
		}
	}
}

func TestFormatAndVersionBitsMatchTheStandardsTables(t *testing.T) {
	for mask, want := range []int{0x5412, 0x5125, 0x5E7C, 0x5B4B, 0x45F9, 0x40CE, 0x4F97, 0x4AA0} {
		if got := formatBits(mask); got != want {
			t.Errorf("format M mask %d: %015b, want %015b", mask, got, want)
		}
	}
	for version, want := range map[int]int{7: 0x07C94, 8: 0x085BC, 9: 0x09A99, 10: 0x0A4D3} {
		if got := versionBits(version); got != want {
			t.Errorf("version %d: %05X, want %05X", version, got, want)
		}
	}
}

func TestTheChosenMaskIsTheOneWrittenInTheFormatBits(t *testing.T) {
	data := []byte("https://fantasy.example/vincular#ABCD-EFGH")
	chosen, err := Encode(data)
	if err != nil {
		t.Fatal(err)
	}
	forced, _ := encode(data, chosen.Mask)
	if chosen.String() != forced.String() {
		t.Errorf("the automatic choice of mask %d drew another matrix", chosen.Mask)
	}
}

func TestTooMuchDataIsRefused(t *testing.T) {
	if _, err := Encode(make([]byte, 214)); !errors.Is(err, ErrTooLong) {
		t.Errorf("214 bytes: %v", err)
	}
	if code, err := Encode(make([]byte, 213)); err != nil || code.Version != 10 {
		t.Errorf("213 bytes, the most 10-M holds: %v", err)
	}
}

func TestTheSVGHasAQuietZoneAndOneSquarePerDarkModule(t *testing.T) {
	code, _ := Encode([]byte("A"))
	svg := code.SVG()
	if !strings.Contains(svg, `viewBox="0 0 29 29"`) {
		t.Errorf("a 21-module code with four on each side: %.120s", svg)
	}
	if got, want := strings.Count(svg, "h1v1h-1z"), strings.Count(code.String(), "#"); got != want {
		t.Errorf("%d squares for %d dark modules", got, want)
	}
}

// Package qr encodes bytes as a QR code: byte mode, level M, versions 1 to 10, drawn as SVG.
package qr

import (
	"errors"
	"fmt"
	"strings"
)

type block struct {
	ec, short, shorts, longs int
}

// Level M of the standard's table: EC codewords per block, data codewords of a short block, short and long blocks.
var levelM = [...]block{
	1: {10, 16, 1, 0}, 2: {16, 28, 1, 0}, 3: {26, 44, 1, 0}, 4: {18, 32, 2, 0}, 5: {24, 43, 2, 0},
	6: {16, 27, 4, 0}, 7: {18, 31, 4, 0}, 8: {22, 38, 2, 2}, 9: {22, 36, 3, 2}, 10: {26, 43, 4, 1},
}

var alignment = [...][]int{
	2: {6, 18}, 3: {6, 22}, 4: {6, 26}, 5: {6, 30}, 6: {6, 34},
	7: {6, 22, 38}, 8: {6, 24, 42}, 9: {6, 26, 46}, 10: {6, 28, 50},
}

const maxVersion = 10

var ErrTooLong = errors.New("qr: too long for version 10-M")

type Code struct {
	Version int
	Mask    int
	Size    int
	dark    [][]bool
	fixed   [][]bool
}

func (c *Code) Dark(x, y int) bool { return c.dark[y][x] }

func dataCapacity(version int) int {
	b := levelM[version]
	return b.short*b.shorts + (b.short+1)*b.longs
}

func countBits(version int) int {
	if version < 10 {
		return 8
	}
	return 16
}

func Encode(data []byte) (*Code, error) {
	return encode(data, -1)
}

func encode(data []byte, mask int) (*Code, error) {
	version := 1
	for ; version <= maxVersion; version++ {
		if 4+countBits(version)+8*len(data) <= 8*dataCapacity(version) {
			break
		}
	}
	if version > maxVersion {
		return nil, ErrTooLong
	}
	codewords := interleave(version, pad(version, data))
	code := newCode(version)
	code.drawFunctions()
	code.place(codewords)
	if mask < 0 {
		best := 0
		for candidate := range 8 {
			code.apply(candidate)
			code.drawFormat(candidate)
			if score := code.penalty(); candidate == 0 || score < best {
				best, mask = score, candidate
			}
			code.apply(candidate)
		}
	}
	code.apply(mask)
	code.drawFormat(mask)
	code.Mask = mask
	return code, nil
}

func pad(version int, data []byte) []byte {
	var bits bitWriter
	bits.write(0b0100, 4)
	bits.write(len(data), countBits(version))
	for _, b := range data {
		bits.write(int(b), 8)
	}
	capacity := 8 * dataCapacity(version)
	bits.write(0, min(4, capacity-bits.length))
	bits.write(0, (8-bits.length%8)%8)
	for filler := 0xEC; bits.length < capacity; filler ^= 0xEC ^ 0x11 {
		bits.write(filler, 8)
	}
	return bits.bytes
}

type bitWriter struct {
	bytes  []byte
	length int
}

func (w *bitWriter) write(value, count int) {
	for i := count - 1; i >= 0; i-- {
		if w.length%8 == 0 {
			w.bytes = append(w.bytes, 0)
		}
		w.bytes[len(w.bytes)-1] |= byte((value>>i)&1) << (7 - w.length%8)
		w.length++
	}
}

func interleave(version int, data []byte) []byte {
	b := levelM[version]
	divisor := generator(b.ec)
	var blocks, checks [][]byte
	for i := range b.shorts + b.longs {
		size := b.short
		if i >= b.shorts {
			size++
		}
		blocks = append(blocks, data[:size])
		checks = append(checks, remainder(data[:size], divisor))
		data = data[size:]
	}
	var out []byte
	for i := range b.short + 1 {
		for _, blk := range blocks {
			if i < len(blk) {
				out = append(out, blk[i])
			}
		}
	}
	for i := range b.ec {
		for _, check := range checks {
			out = append(out, check[i])
		}
	}
	return out
}

func multiply(x, y byte) byte {
	z := 0
	for i := 7; i >= 0; i-- {
		z = (z << 1) ^ ((z >> 7) * 0x11D)
		z ^= int((y>>i)&1) * int(x)
	}
	return byte(z)
}

func generator(degree int) []byte {
	result := make([]byte, degree)
	result[degree-1] = 1
	root := byte(1)
	for range degree {
		for j := range result {
			result[j] = multiply(result[j], root)
			if j+1 < len(result) {
				result[j] ^= result[j+1]
			}
		}
		root = multiply(root, 2)
	}
	return result
}

func remainder(data, divisor []byte) []byte {
	result := make([]byte, len(divisor))
	for _, b := range data {
		factor := b ^ result[0]
		copy(result, result[1:])
		result[len(result)-1] = 0
		for i := range result {
			result[i] ^= multiply(divisor[i], factor)
		}
	}
	return result
}

func newCode(version int) *Code {
	size := 17 + 4*version
	code := &Code{Version: version, Size: size}
	for range size {
		code.dark = append(code.dark, make([]bool, size))
		code.fixed = append(code.fixed, make([]bool, size))
	}
	return code
}

func (c *Code) set(x, y int, dark bool) {
	c.dark[y][x] = dark
	c.fixed[y][x] = true
}

func (c *Code) drawFunctions() {
	for i := range c.Size {
		c.set(6, i, i%2 == 0)
		c.set(i, 6, i%2 == 0)
	}
	for _, corner := range [][2]int{{3, 3}, {c.Size - 4, 3}, {3, c.Size - 4}} {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				x, y := corner[0]+dx, corner[1]+dy
				if x >= 0 && x < c.Size && y >= 0 && y < c.Size {
					ring := max(abs(dx), abs(dy))
					c.set(x, y, ring != 2 && ring != 4)
				}
			}
		}
	}
	centres := alignment[c.Version]
	for i, cy := range centres {
		for j, cx := range centres {
			last := len(centres) - 1
			if (i == 0 && j == 0) || (i == 0 && j == last) || (i == last && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					c.set(cx+dx, cy+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	c.drawFormat(0)
	if c.Version >= 7 {
		bits := versionBits(c.Version)
		for i := range 18 {
			dark := (bits>>i)&1 == 1
			a, b := c.Size-11+i%3, i/3
			c.set(a, b, dark)
			c.set(b, a, dark)
		}
	}
}

func formatBits(mask int) int {
	data := 0b00<<3 | mask
	rem := data
	for range 10 {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	return (data<<10 | rem) ^ 0x5412
}

func versionBits(version int) int {
	rem := version
	for range 12 {
		rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
	}
	return version<<12 | rem
}

func (c *Code) drawFormat(mask int) {
	bits := formatBits(mask)
	bit := func(i int) bool { return (bits>>i)&1 == 1 }
	for i := range 6 {
		c.set(8, i, bit(i))
	}
	c.set(8, 7, bit(6))
	c.set(8, 8, bit(7))
	c.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		c.set(14-i, 8, bit(i))
	}
	for i := range 8 {
		c.set(c.Size-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		c.set(8, c.Size-15+i, bit(i))
	}
	c.set(8, c.Size-8, true)
}

func (c *Code) place(codewords []byte) {
	i := 0
	for right := c.Size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vertical := range c.Size {
			for j := range 2 {
				x := right - j
				y := vertical
				if (right+1)&2 == 0 {
					y = c.Size - 1 - vertical
				}
				if !c.fixed[y][x] && i < len(codewords)*8 {
					c.dark[y][x] = (codewords[i>>3]>>(7-i&7))&1 == 1
					i++
				}
			}
		}
	}
}

func masked(mask, x, y int) bool {
	switch mask {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (x/3+y/2)%2 == 0
	case 5:
		return x*y%2+x*y%3 == 0
	case 6:
		return (x*y%2+x*y%3)%2 == 0
	default:
		return ((x+y)%2+x*y%3)%2 == 0
	}
}

func (c *Code) apply(mask int) {
	for y := range c.Size {
		for x := range c.Size {
			if !c.fixed[y][x] && masked(mask, x, y) {
				c.dark[y][x] = !c.dark[y][x]
			}
		}
	}
}

func (c *Code) penalty() int {
	score, dark := 0, 0
	finder := []bool{true, false, true, true, true, false, true}
	for _, line := range c.lines() {
		run := 1
		for i := 1; i <= len(line); i++ {
			if i < len(line) && line[i] == line[i-1] {
				run++
				continue
			}
			if run >= 5 {
				score += run - 2
			}
			run = 1
		}
		for i := 0; i+7 <= len(line); i++ {
			if !matches(line[i:i+7], finder) {
				continue
			}
			if light(line, i-4, i) || light(line, i+7, i+11) {
				score += 40
			}
		}
	}
	for y := range c.Size {
		for x := range c.Size {
			if c.dark[y][x] {
				dark++
			}
			if x+1 < c.Size && y+1 < c.Size && c.dark[y][x] == c.dark[y][x+1] &&
				c.dark[y][x] == c.dark[y+1][x] && c.dark[y][x] == c.dark[y+1][x+1] {
				score += 3
			}
		}
	}
	total := c.Size * c.Size
	return score + 10*(abs(dark*20-total*10)/total)
}

func (c *Code) lines() [][]bool {
	var lines [][]bool
	for y := range c.Size {
		lines = append(lines, c.dark[y])
	}
	for x := range c.Size {
		column := make([]bool, c.Size)
		for y := range c.Size {
			column[y] = c.dark[y][x]
		}
		lines = append(lines, column)
	}
	return lines
}

func matches(line, pattern []bool) bool {
	for i := range pattern {
		if line[i] != pattern[i] {
			return false
		}
	}
	return true
}

// Past the edge counts as light, as the quiet zone around the code is.
func light(line []bool, from, to int) bool {
	for i := from; i < to; i++ {
		if i >= 0 && i < len(line) && line[i] {
			return false
		}
	}
	return true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (c *Code) SVG() string {
	const quiet = 4
	side := c.Size + 2*quiet
	var path strings.Builder
	for y := range c.Size {
		for x := range c.Size {
			if c.dark[y][x] {
				fmt.Fprintf(&path, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+
		`<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="%s"/></svg>`, side, side, side, side, path.String())
}

func (c *Code) String() string {
	var out strings.Builder
	for y := range c.Size {
		for x := range c.Size {
			if c.dark[y][x] {
				out.WriteByte('#')
			} else {
				out.WriteByte('.')
			}
		}
		out.WriteByte('\n')
	}
	return out.String()
}

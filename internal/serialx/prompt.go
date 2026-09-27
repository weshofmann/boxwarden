package serialx

import "bytes"

// The current Ubuntu clone logs boxwarden into bash on hvc0 after firstboot
// identity regeneration. Its default home-directory prompt is a scheduling
// signal only; trust still comes from the nonce-bound helper response.
const maxPromptLineBytes = 256

var promptPrefix = []byte("boxwarden@boxwarden-")
var promptSuffix = []byte(":~$ ")

type promptScanner struct {
	line   []byte
	escape uint8
}

func (p *promptScanner) feed(input []byte) bool {
	for _, b := range input {
		switch p.escape {
		case 1:
			p.escape = 0
			if b == '[' {
				p.escape = 2
			}
			continue
		case 2:
			if b >= 0x40 && b <= 0x7e {
				p.escape = 0
			}
			continue
		}
		if b == 0x1b {
			p.escape = 1
			continue
		}
		if b == '\r' || b == '\n' {
			p.line = p.line[:0]
			continue
		}
		if b == '\b' || b == 0x7f {
			if len(p.line) > 0 {
				p.line = p.line[:len(p.line)-1]
			}
			continue
		}
		if b < 0x20 || b > 0x7e {
			continue
		}
		if len(p.line) == maxPromptLineBytes {
			copy(p.line, p.line[1:])
			p.line = p.line[:maxPromptLineBytes-1]
		}
		p.line = append(p.line, b)
		if exactLoginPrompt(p.line) {
			return true
		}
	}
	return false
}

func exactLoginPrompt(line []byte) bool {
	const machineIDChars = 12
	start := bytes.LastIndex(line, promptPrefix)
	if start < 0 {
		return false
	}
	tail := line[start+len(promptPrefix):]
	if len(tail) != machineIDChars+len(promptSuffix) || !bytes.Equal(tail[machineIDChars:], promptSuffix) {
		return false
	}
	for _, b := range tail[:machineIDChars] {
		if b < '0' || b > '9' {
			if b < 'a' || b > 'f' {
				return false
			}
		}
	}
	return true
}

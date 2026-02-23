package parser

// ANSIState represents the state of the ANSI stripping state machine.
type ANSIState int

const (
	stateNormal ANSIState = iota
	stateEscape           // Saw ESC (0x1B)
	stateCSI              // Saw ESC [
	stateOSC              // Saw ESC ]
	stateOSCST            // Saw ESC inside OSC (possible ST = ESC \)
)

// OSCMarker represents an extracted OSC 133 shell integration marker.
type OSCMarker struct {
	Code     byte   // 'A', 'C', 'D'
	Param    string // e.g. exit code for 'D'
	Position int    // byte position in the stripped output where this marker occurred
}

// ANSIStripper processes raw terminal bytes, strips ANSI escape sequences,
// and extracts OSC 133 shell integration markers as structured metadata.
type ANSIStripper struct {
	state ANSIState

	// CSI accumulator
	csiParams []byte

	// OSC accumulator
	oscData []byte

	// Alternate screen flag
	AltScreen bool

	// Output buffers per Feed call
	stripped []byte
	markers  []OSCMarker

	// Current position in stripped output (cumulative across feeds)
	strippedPos int

	// Line buffer for \r handling
	lineBuf []byte
	linePos int
}

// NewANSIStripper creates a new ANSI stripping state machine.
func NewANSIStripper() *ANSIStripper {
	return &ANSIStripper{
		state:   stateNormal,
		lineBuf: make([]byte, 0, 256),
	}
}

// Feed processes raw bytes and returns stripped text and any OSC 133 markers found.
// The stripped text has all ANSI sequences removed except OSC 133 markers
// (which are extracted as metadata).
func (a *ANSIStripper) Feed(data []byte) (stripped []byte, markers []OSCMarker) {
	a.stripped = a.stripped[:0]
	a.markers = a.markers[:0]

	for _, b := range data {
		a.processByte(b)
	}

	// Copy to avoid aliasing
	out := make([]byte, len(a.stripped))
	copy(out, a.stripped)

	var outMarkers []OSCMarker
	if len(a.markers) > 0 {
		outMarkers = make([]OSCMarker, len(a.markers))
		copy(outMarkers, a.markers)
	}

	return out, outMarkers
}

func (a *ANSIStripper) processByte(b byte) {
	switch a.state {
	case stateNormal:
		a.processNormal(b)
	case stateEscape:
		a.processEscape(b)
	case stateCSI:
		a.processCSI(b)
	case stateOSC:
		a.processOSC(b)
	case stateOSCST:
		a.processOSCST(b)
	}
}

func (a *ANSIStripper) processNormal(b byte) {
	switch {
	case b == 0x1B: // ESC
		a.state = stateEscape
	case b == '\n':
		a.emitByte('\n')
	case b == '\r':
		a.emitByte('\r')
	case b == '\b': // Backspace
		a.emitByte('\b')
	case b == '\t': // Tab
		a.emitByte('\t')
	case b == 0x07: // BEL — ignore in normal mode
		// Discard standalone BEL
	case b >= 0x20 || b == 0: // Printable or null
		if b >= 0x20 {
			a.emitByte(b)
		}
		// Discard null bytes
	default:
		// Discard other C0 control characters (except the ones handled above)
	}
}

func (a *ANSIStripper) processEscape(b byte) {
	switch b {
	case '[': // CSI
		a.state = stateCSI
		a.csiParams = a.csiParams[:0]
	case ']': // OSC
		a.state = stateOSC
		a.oscData = a.oscData[:0]
	default:
		// Unknown escape sequence — discard and return to normal
		a.state = stateNormal
	}
}

func (a *ANSIStripper) processCSI(b byte) {
	if b >= 0x30 && b <= 0x3F {
		// Parameter bytes (0-9, ;, <, =, >, ?)
		a.csiParams = append(a.csiParams, b)
		return
	}
	if b >= 0x20 && b <= 0x2F {
		// Intermediate bytes
		a.csiParams = append(a.csiParams, b)
		return
	}
	if b >= 0x40 && b <= 0x7E {
		// Final byte — sequence complete
		a.handleCSI(b)
		a.state = stateNormal
		return
	}
	// Invalid — abort CSI
	a.state = stateNormal
}

func (a *ANSIStripper) handleCSI(finalByte byte) {
	// Detect alternate screen mode
	params := string(a.csiParams)
	if finalByte == 'h' && params == "?1049" {
		a.AltScreen = true
	} else if finalByte == 'l' && params == "?1049" {
		a.AltScreen = false
	}
	// All CSI sequences are stripped (not emitted to output)
}

func (a *ANSIStripper) processOSC(b byte) {
	switch b {
	case 0x07: // BEL terminates OSC
		a.handleOSC()
		a.state = stateNormal
	case 0x1B: // Possible ST (ESC \)
		a.state = stateOSCST
	default:
		if len(a.oscData) < 4096 { // Safety limit
			a.oscData = append(a.oscData, b)
		}
	}
}

func (a *ANSIStripper) processOSCST(b byte) {
	if b == '\\' {
		// ST = ESC \ — terminates OSC
		a.handleOSC()
		a.state = stateNormal
	} else {
		// Not an ST — the ESC was part of something else.
		// Discard accumulated OSC and process this byte as normal.
		a.state = stateNormal
		a.processByte(b)
	}
}

func (a *ANSIStripper) handleOSC() {
	data := string(a.oscData)

	// Check for OSC 133 shell integration markers
	if len(data) >= 4 && data[:4] == "133;" {
		rest := data[4:]
		if len(rest) >= 1 {
			code := rest[0]
			param := ""
			if len(rest) > 1 && rest[1] == ';' {
				param = rest[2:]
			}

			switch code {
			case 'A', 'C', 'D':
				a.markers = append(a.markers, OSCMarker{
					Code:     code,
					Param:    param,
					Position: a.strippedPos + len(a.stripped),
				})
			}
		}
	}
	// All OSC sequences (including 133) are stripped from visible output
}

func (a *ANSIStripper) emitByte(b byte) {
	a.stripped = append(a.stripped, b)
}

// Reset clears the stripper state (but preserves AltScreen).
func (a *ANSIStripper) Reset() {
	a.state = stateNormal
	a.csiParams = a.csiParams[:0]
	a.oscData = a.oscData[:0]
	a.stripped = a.stripped[:0]
	a.markers = a.markers[:0]
}

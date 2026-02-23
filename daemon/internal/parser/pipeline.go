package parser

import (
	"sync"
	"time"
)

const (
	// defaultFlushTimeout is the line accumulator flush timeout for prompts without \n.
	defaultFlushTimeout = 500 * time.Millisecond

	// outputChunkInterval is how often to flush accumulated output as streaming events.
	outputChunkInterval = 200 * time.Millisecond

	// outputChunkMaxBytes is the max stripped text to buffer before flushing.
	outputChunkMaxBytes = 4096
)

// Pipeline wires the parser stages together:
// raw bytes → ANSI strip → line accumulate → conversation parse → event emit.
// When an AI tool is the foreground process, lines are also fed to the
// appropriate AI enhancement parser which emits supplemental ai_* events.
type Pipeline struct {
	stripper     *ANSIStripper
	accumulator  *Accumulator
	conversation *ConversationParser

	// AI enhancement parsers (nil when inactive)
	claudeParser *ClaudeParser
	aiderParser  *AiderParser
	activeAI     string // current tool_type driving AI parsing ("claude_code", "aider", "")

	events chan Event

	// Output chunk flushing
	chunkTicker *time.Ticker
	chunkBytes  int

	mu     sync.Mutex
	closed bool
}

// NewPipeline creates a new parser pipeline that processes raw PTY output
// and user input into structured conversation events.
func NewPipeline() *Pipeline {
	events := make(chan Event, 256)

	var conv *ConversationParser
	var accum *Accumulator

	// Event callback: conversation parser emits events to the channel
	emitEvent := func(e Event) {
		select {
		case events <- e:
		default:
			// Channel full — drop event rather than block.
			// In practice with a 256-buffer this shouldn't happen.
		}
	}

	conv = NewConversationParser(emitEvent)

	// AI parsers share the same emit callback so their events go to the same channel
	claudeP := NewClaudeParser(emitEvent)
	aiderP := NewAiderParser(emitEvent)

	// Line callback: feed lines to conversation parser and active AI parser
	p := &Pipeline{
		stripper:     NewANSIStripper(),
		conversation: conv,
		claudeParser: claudeP,
		aiderParser:  aiderP,
		events:       events,
		chunkTicker:  time.NewTicker(outputChunkInterval),
	}

	accum = NewAccumulator(defaultFlushTimeout, func(line Line) {
		// Always feed the base conversation parser
		conv.FeedLine(line)

		// Also feed the active AI parser if one is active
		// Only feed text lines (markers are for the conversation parser)
		if line.Text != "" {
			switch p.activeAI {
			case "claude_code":
				p.claudeParser.FeedLine(line)
			case "aider":
				p.aiderParser.FeedLine(line)
			}
		}
	})

	p.accumulator = accum

	// Background goroutine for periodic output flushing
	go p.chunkFlusher()

	return p
}

// Events returns the channel on which parsed events are emitted.
func (p *Pipeline) Events() <-chan Event {
	return p.events
}

// FeedOutput feeds raw PTY output bytes into the pipeline.
func (p *Pipeline) FeedOutput(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}

	// Stage 1: ANSI stripping
	stripped, markers := p.stripper.Feed(data)

	// Check for alternate screen transitions
	if p.stripper.AltScreen {
		p.accumulator.SetPaused(true)
	} else {
		p.accumulator.SetPaused(false)
	}

	// Stage 2: Line accumulation
	p.accumulator.Feed(stripped, markers)

	// Track chunk bytes for size-based flushing
	p.chunkBytes += len(stripped)
	if p.chunkBytes >= outputChunkMaxBytes {
		p.conversation.FlushPending()
		p.chunkBytes = 0
	}
}

// FeedInput feeds raw user input bytes into the pipeline.
func (p *Pipeline) FeedInput(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}

	p.conversation.FeedInput(data)
}

// SetCWD updates the current working directory context.
func (p *Pipeline) SetCWD(cwd string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conversation.SetCWD(cwd)
}

// SetProcess updates the foreground process context and activates/deactivates
// the appropriate AI enhancement parser.
func (p *Pipeline) SetProcess(name, toolType string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// If AI tool type changed, flush and reset the old AI parser
	if p.activeAI != toolType {
		switch p.activeAI {
		case "claude_code":
			p.claudeParser.Flush()
			p.claudeParser.Reset()
		case "aider":
			p.aiderParser.Flush()
			p.aiderParser.Reset()
		}
		p.activeAI = toolType
	}

	p.conversation.SetProcess(name, toolType)
}

// State returns the current conversation parser state.
func (p *Pipeline) State() ConversationState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conversation.State()
}

func (p *Pipeline) chunkFlusher() {
	for range p.chunkTicker.C {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}
		p.conversation.FlushPending()
		p.chunkBytes = 0
		p.mu.Unlock()
	}
}

// Close shuts down the pipeline and flushes remaining data.
func (p *Pipeline) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}
	p.closed = true

	p.chunkTicker.Stop()

	// Flush AI parsers before closing
	switch p.activeAI {
	case "claude_code":
		p.claudeParser.Flush()
	case "aider":
		p.aiderParser.Flush()
	}

	p.accumulator.Close()
	p.conversation.Close()
	close(p.events)
}

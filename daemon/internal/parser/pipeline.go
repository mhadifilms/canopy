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
type Pipeline struct {
	stripper     *ANSIStripper
	accumulator  *Accumulator
	conversation *ConversationParser

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

	accum = NewAccumulator(defaultFlushTimeout, func(line Line) {
		conv.FeedLine(line)
	})

	stripper := NewANSIStripper()

	p := &Pipeline{
		stripper:     stripper,
		accumulator:  accum,
		conversation: conv,
		events:       events,
		chunkTicker:  time.NewTicker(outputChunkInterval),
	}

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

// SetProcess updates the foreground process context.
func (p *Pipeline) SetProcess(name, toolType string) {
	p.mu.Lock()
	defer p.mu.Unlock()
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
	p.accumulator.Close()
	p.conversation.Close()
	close(p.events)
}

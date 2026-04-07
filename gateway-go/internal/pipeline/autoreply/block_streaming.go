package autoreply

import (
	"strings"
	"sync"
)

// BlockStreamMode defines how blocks are delivered.
type BlockStreamMode string

const (
	BlockStreamOff    BlockStreamMode = "off"
	BlockStreamSimple BlockStreamMode = "simple"
	BlockStreamFull   BlockStreamMode = "full"
)

// StreamBlock represents a single block in a streaming response.
type StreamBlock struct {
	Type      string `json:"type"` // "text", "code", "thinking", "tool_use", "tool_result"
	Content   string `json:"content,omitempty"`
	Language  string `json:"language,omitempty"` // for code blocks
	ToolName  string `json:"toolName,omitempty"`
	ToolID    string `json:"toolId,omitempty"`
	IsPartial bool   `json:"isPartial,omitempty"`
}

// BlockCoalescer merges streaming text chunks into complete blocks.
type BlockCoalescer struct {
	mu        sync.Mutex
	pending   strings.Builder
	blocks    []StreamBlock
	inFence   bool
	fenceLang string
}

// NewBlockCoalescer creates a new block coalescer.
func NewBlockCoalescer() *BlockCoalescer {
	return &BlockCoalescer{}
}

// Feed adds new streamed text to the coalescer.
func (c *BlockCoalescer) Feed(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending.WriteString(text)
}

// Flush processes all pending text and returns complete blocks.
func (c *BlockCoalescer) Flush() []StreamBlock {
	c.mu.Lock()
	defer c.mu.Unlock()

	text := c.pending.String()
	c.pending.Reset()

	if text == "" {
		return nil
	}

	// Simple block detection: split on code fences.
	// Use IndexByte scanning instead of strings.Split to avoid allocating a
	// []string for every Flush call (which is invoked on every streamed chunk).
	var blocks []StreamBlock
	var current strings.Builder
	remaining := text

	for remaining != "" {
		var line string
		if idx := strings.IndexByte(remaining, '\n'); idx >= 0 {
			line = remaining[:idx]
			remaining = remaining[idx+1:]
		} else {
			line = remaining
			remaining = ""
		}

		trimmed := strings.TrimSpace(line)

		// Detect code fence start/end.
		if strings.HasPrefix(trimmed, "```") {
			if c.inFence {
				// End of code block.
				blocks = append(blocks, StreamBlock{
					Type:     "code",
					Content:  current.String(),
					Language: c.fenceLang,
				})
				current.Reset()
				c.inFence = false
				c.fenceLang = ""
				continue
			}
			// Start of code block — flush text before it.
			if current.Len() > 0 {
				blocks = append(blocks, StreamBlock{
					Type:    "text",
					Content: current.String(),
				})
				current.Reset()
			}
			c.inFence = true
			c.fenceLang = strings.TrimPrefix(trimmed, "```")
			c.fenceLang = strings.TrimSpace(c.fenceLang)
			continue
		}

		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}

	// Remaining text.
	if current.Len() > 0 {
		blockType := "text"
		if c.inFence {
			blockType = "code"
		}
		blocks = append(blocks, StreamBlock{
			Type:      blockType,
			Content:   current.String(),
			Language:  c.fenceLang,
			IsPartial: c.inFence,
		})
	}

	c.blocks = append(c.blocks, blocks...)
	return blocks
}

// AllBlocks returns all coalesced blocks.
func (c *BlockCoalescer) AllBlocks() []StreamBlock {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]StreamBlock, len(c.blocks))
	copy(result, c.blocks)
	return result
}

// BlockReplyPipeline assembles streamed blocks into final reply payloads.
type BlockReplyPipeline struct {
	coalescer *BlockCoalescer
	onBlock   func(block StreamBlock)
}

// NewBlockReplyPipeline creates a new block reply pipeline.
func NewBlockReplyPipeline(onBlock func(block StreamBlock)) *BlockReplyPipeline {
	return &BlockReplyPipeline{
		coalescer: NewBlockCoalescer(),
		onBlock:   onBlock,
	}
}

// Feed adds streamed text to the pipeline.
func (p *BlockReplyPipeline) Feed(text string) {
	p.coalescer.Feed(text)
	blocks := p.coalescer.Flush()
	for _, b := range blocks {
		if p.onBlock != nil && !b.IsPartial {
			p.onBlock(b)
		}
	}
}

// Finalize flushes any remaining partial blocks.
func (p *BlockReplyPipeline) Finalize() []StreamBlock {
	return p.coalescer.Flush()
}

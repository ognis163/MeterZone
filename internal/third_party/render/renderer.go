// Package render provides interfaces and implementations for drawing
// laid-out document content to an output target such as a PDF file.
package render

import (
	"github.com/gpdf-dev/gpdf/document"
	"github.com/gpdf-dev/gpdf/pdf"
)

// Renderer defines the operations needed to draw a laid-out document.
// Implementations translate high-level drawing commands into format-specific
// output (e.g., PDF content stream operators).
type Renderer interface {
	// BeginDocument initializes the output and writes document-level metadata.
	BeginDocument(info document.DocumentMetadata) error
	// BeginPage starts a new page with the given dimensions.
	BeginPage(size document.Size) error
	// EndPage finishes the current page and flushes its content.
	EndPage() error
	// RenderText draws a string of text at the given position with the
	// specified style (font, size, color, alignment).
	RenderText(text string, pos document.Point, style document.Style) error
	// RenderRect draws a rectangle with the given fill and stroke properties.
	RenderRect(rect document.Rectangle, style RectStyle) error
	// RenderImage draws an image at the given position and size.
	RenderImage(src document.ImageSource, pos document.Point, size document.Size) error
	// RenderPath draws a path with the given fill and/or stroke style.
	RenderPath(path document.Path, style PathStyle) error
	// RenderLine draws a straight line between two points.
	RenderLine(from, to document.Point, style LineStyle) error
	// EndDocument finalizes the output (e.g., writes cross-reference table).
	EndDocument() error
}

// RectStyle controls the visual appearance of a rendered rectangle.
type RectStyle struct {
	// FillColor is the interior fill color; nil means no fill.
	FillColor *pdf.Color
	// StrokeColor is the border color; nil means no stroke.
	StrokeColor *pdf.Color
	// StrokeWidth is the border line width in points.
	StrokeWidth float64
}

// PathStyle controls the visual appearance of a rendered path.
type PathStyle struct {
	// FillColor is the interior fill color; nil means no fill.
	FillColor *pdf.Color
	// StrokeColor is the stroke color; nil means no stroke.
	StrokeColor *pdf.Color
	// StrokeWidth is the line width in points.
	StrokeWidth float64
	// DashPattern defines the lengths of dashes and gaps (e.g. [3 2]).
	DashPattern []float64
	// DashPhase is the offset into DashPattern at which the pattern starts.
	DashPhase float64
}

// LineStyle controls the visual appearance of a rendered line.
type LineStyle struct {
	// Color is the stroke color.
	Color pdf.Color
	// Width is the line width in points.
	Width float64
	// DashPattern defines the lengths of dashes and gaps (e.g. [3 2]).
	DashPattern []float64
	// DashPhase is the offset into DashPattern at which the pattern starts.
	DashPhase float64
}

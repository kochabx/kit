package qr

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"strings"

	qrcode "github.com/yeqown/go-qrcode/v2"

	"github.com/kochabx/kit/x/auth/mfa"
)

const (
	defaultModuleSize = 8
	defaultMargin     = 4
)

type options struct {
	moduleSize int
	margin     int
}

// Option customizes QR image rendering.
type Option func(*options)

// WithModuleSize sets the width/height in pixels of each QR module (default 8).
func WithModuleSize(px int) Option {
	return func(o *options) {
		if px > 0 {
			o.moduleSize = px
		}
	}
}

// WithMargin sets the quiet-zone margin in QR modules (default 4).
func WithMargin(modules int) Option {
	return func(o *options) {
		if modules >= 0 {
			o.margin = modules
		}
	}
}

func resolveOptions(opts ...Option) options {
	o := options{
		moduleSize: defaultModuleSize,
		margin:     defaultMargin,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// PNG renders an otpauth URI (or arbitrary text) into PNG bytes without GUI/font bloat.
func PNG(uri string, opts ...Option) ([]byte, error) {
	if uri == "" {
		return nil, errors.New("qr: uri is required")
	}
	o := resolveOptions(opts...)
	qrc, err := qrcode.New(uri)
	if err != nil {
		return nil, fmt.Errorf("qr: encode uri: %w", err)
	}

	var buf bytes.Buffer
	w := &pngWriter{
		buf:        &buf,
		moduleSize: o.moduleSize,
		margin:     o.margin,
	}
	if err := qrc.Save(w); err != nil {
		return nil, fmt.Errorf("qr: render png: %w", err)
	}
	return buf.Bytes(), nil
}

// EnrollmentPNG renders PNG QR bytes for a previously generated MFA Enrollment.
func EnrollmentPNG(e mfa.Enrollment, opts ...Option) ([]byte, error) {
	return PNG(e.URI, opts...)
}

// SVG renders an otpauth URI into clean scalable vector markup.
func SVG(uri string, opts ...Option) ([]byte, error) {
	if uri == "" {
		return nil, errors.New("qr: uri is required")
	}
	o := resolveOptions(opts...)
	qrc, err := qrcode.New(uri)
	if err != nil {
		return nil, fmt.Errorf("qr: encode uri: %w", err)
	}

	var buf bytes.Buffer
	w := &svgWriter{
		buf:    &buf,
		margin: o.margin,
	}
	if err := qrc.Save(w); err != nil {
		return nil, fmt.Errorf("qr: render svg: %w", err)
	}
	return buf.Bytes(), nil
}

// EnrollmentSVG renders SVG QR markup for a previously generated MFA Enrollment.
func EnrollmentSVG(e mfa.Enrollment, opts ...Option) ([]byte, error) {
	return SVG(e.URI, opts...)
}

type pngWriter struct {
	buf        *bytes.Buffer
	moduleSize int
	margin     int
}

func (w *pngWriter) Write(mat qrcode.Matrix) error {
	side := (mat.Width() + 2*w.margin) * w.moduleSize
	img := image.NewPaletted(image.Rect(0, 0, side, side), color.Palette{color.White, color.Black})

	mat.Iterate(qrcode.IterDirection_COLUMN, func(x, y int, v qrcode.QRValue) {
		if !v.IsSet() {
			return
		}
		px := (x + w.margin) * w.moduleSize
		py := (y + w.margin) * w.moduleSize
		for dy := range w.moduleSize {
			for dx := range w.moduleSize {
				img.SetColorIndex(px+dx, py+dy, 1)
			}
		}
	})
	return png.Encode(w.buf, img)
}

func (w *pngWriter) Close() error { return nil }

type svgWriter struct {
	buf    *bytes.Buffer
	margin int
}

func (w *svgWriter) Write(mat qrcode.Matrix) error {
	size := mat.Width() + 2*w.margin
	var sb strings.Builder
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `)
	sb.WriteString(strconv.Itoa(size))
	sb.WriteByte(' ')
	sb.WriteString(strconv.Itoa(size))
	sb.WriteString(`" shape-rendering="crispEdges">`)
	sb.WriteString(`<rect width="100%" height="100%" fill="#ffffff"/>`)
	sb.WriteString(`<path fill="#000000" d="`)

	mat.Iterate(qrcode.IterDirection_ROW, func(x, y int, v qrcode.QRValue) {
		if !v.IsSet() {
			return
		}
		sb.WriteByte('M')
		sb.WriteString(strconv.Itoa(x + w.margin))
		sb.WriteByte(' ')
		sb.WriteString(strconv.Itoa(y + w.margin))
		sb.WriteString("h1v1h-1z")
	})

	sb.WriteString(`"/></svg>`)
	_, err := w.buf.WriteString(sb.String())
	return err
}

func (w *svgWriter) Close() error { return nil }

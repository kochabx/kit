package qr_test

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"github.com/kochabx/kit/x/auth/mfa"
	"github.com/kochabx/kit/x/auth/mfa/qr"
)

func TestPNGAndSVGRendering(t *testing.T) {
	t.Parallel()

	if _, err := qr.PNG(""); err == nil {
		t.Fatal("expected error for empty URI in PNG")
	}
	if _, err := qr.SVG(""); err == nil {
		t.Fatal("expected error for empty URI in SVG")
	}

	totp, err := mfa.NewTOTP(mfa.TOTPConfig{Issuer: "Kit"})
	if err != nil {
		t.Fatal(err)
	}
	enr, err := totp.Generate("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}

	pngBytes, err := qr.EnrollmentPNG(enr, qr.WithModuleSize(4), qr.WithMargin(2))
	if err != nil {
		t.Fatalf("EnrollmentPNG failed: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(pngBytes)); err != nil {
		t.Fatalf("invalid PNG output: %v", err)
	}

	svgBytes, err := qr.EnrollmentSVG(enr, qr.WithMargin(2))
	if err != nil {
		t.Fatalf("EnrollmentSVG failed: %v", err)
	}
	svgStr := string(svgBytes)
	if !strings.HasPrefix(svgStr, "<svg ") || !strings.HasSuffix(svgStr, "</svg>") {
		t.Fatalf("unexpected SVG output: %q", svgStr)
	}
}

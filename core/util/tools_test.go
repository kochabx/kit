package util

import (
	"testing"
)

func TestTools(t *testing.T) {
	t.Log("Id:", Id())
	t.Log("IpV4:", IpV4())
	t.Log("Hostname:", Hostname())
	t.Log("Qrcode:", GenerateRandomCode(6))
	qrcode, err := QRCode("12345", 256)
	if err != nil {
		t.Error(err)
	}
	t.Log("GenerateQRCodeBytes:", qrcode)
}

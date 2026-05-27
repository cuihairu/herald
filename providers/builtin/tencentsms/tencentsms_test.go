package tencentsms

import (
	"net/http"
	"testing"
)

func TestCalculateAuthorization(t *testing.T) {
	// Based on Tencent Cloud official TC3-HMAC-SHA256 example
	// https://cloud.tencent.com/document/api/213/30654
	p := &Provider{
		secretID:  "AKID_FAKE_TEST_SECRET_ID_FOR_UNIT_TEST",
		secretKey: "FAKE_TEST_SECRET_KEY_FOR_UNIT_TEST_ONLY",
		region:    "ap-guangzhou",
		endpoint:  "cvm.tencentcloudapi.com",
	}

	// Use fixed timestamp: 2018-06-08 16:04:03 UTC (unix 1528472643)
	req := &http.Request{
		Method: "POST",
		Header: http.Header{},
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", "cvm.tencentcloudapi.com")
	req.Header.Set("X-TC-Action", "DescribeInstances")
	req.Header.Set("X-TC-Timestamp", "1528472643")
	req.Header.Set("X-TC-Version", "2017-03-12")

	body := `{"Limit": 1, "Filters": [{"Values": ["未命名"], "Name": "instance-name"}]}`

	auth := p.calculateAuthorization(req, body)

	// The signature should contain the correct credential scope (provider uses "sms" service)
	expectedScope := "2018-06-08/sms/tc3_request"
	if !contains(auth, expectedScope) {
		t.Errorf("expected credential scope %q in authorization, got %q", expectedScope, auth)
	}

	// Verify the date conversion from unix timestamp
	// 1528472643 = 2018-06-08 16:04:03 UTC → date = 2018-06-08
	if !contains(auth, "2018-06-08") {
		t.Error("expected date 2018-06-08 in authorization")
	}

	// Verify signature is a valid 64-char hex string
	sig := extractSignature(auth)
	if len(sig) != 64 {
		t.Errorf("expected 64-char hex signature, got %d chars: %s", len(sig), sig)
	}
}

func extractSignature(auth string) string {
	prefix := "Signature="
	for i := 0; i < len(auth); i++ {
		if i+len(prefix) <= len(auth) && auth[i:i+len(prefix)] == prefix {
			return auth[i+len(prefix):]
		}
	}
	return ""
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

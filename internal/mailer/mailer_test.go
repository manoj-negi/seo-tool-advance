package mailer

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestBuildHTMLMessage(t *testing.T) {
	msg := string(buildHTMLMessage("from@x.com", "to@y.com", "Hello", "<b>hi</b>"))

	for _, want := range []string{
		"From: from@x.com\r\n",
		"To: to@y.com\r\n",
		"Content-Type: text/html; charset=\"UTF-8\"\r\n",
		"<b>hi</b>",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\nfull message:\n%s", want, msg)
		}
	}
}

func TestBuildAttachmentMessage(t *testing.T) {
	data := []byte("%PDF-1.4 fake pdf content for testing")
	msg := string(buildAttachmentMessage("from@x.com", "to@y.com", "Report", "<p>see attached</p>", Attachment{
		Filename: "report.pdf",
		MIMEType: "application/pdf",
		Data:     data,
	}))

	if !strings.Contains(msg, "multipart/mixed") {
		t.Error("expected a multipart/mixed content type for an email with an attachment")
	}
	if !strings.Contains(msg, `filename="report.pdf"`) {
		t.Error("expected the attachment filename in Content-Disposition")
	}
	if !strings.Contains(msg, "<p>see attached</p>") {
		t.Error("expected the HTML body to be present")
	}

	// The attachment bytes must round-trip through base64 correctly.
	wantEncoded := base64.StdEncoding.EncodeToString(data)
	gotJoined := strings.ReplaceAll(extractBase64Block(msg), "\r\n", "")
	if gotJoined != wantEncoded {
		t.Errorf("attachment base64 mismatch:\ngot:  %s\nwant: %s", gotJoined, wantEncoded)
	}
}

// extractBase64Block pulls out the base64 body between the attachment's
// blank-line-terminated headers and the closing boundary marker.
func extractBase64Block(msg string) string {
	idx := strings.LastIndex(msg, "Content-Disposition:")
	rest := msg[idx:]
	afterHeaders := rest[strings.Index(rest, "\r\n\r\n")+4:]
	end := strings.Index(afterHeaders, "--seo-crawler-boundary")
	return afterHeaders[:end]
}

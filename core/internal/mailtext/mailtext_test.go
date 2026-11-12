package mailtext

import (
	"strings"
	"testing"
)

func TestExtractPreferredBodyPrefersRichHTMLOverDegradedPlain(t *testing.T) {
	htmlBody := `<html><head><style>.btn{color:red}</style></head><body>
		<div style="display:none;max-height:0px">Hidden preheader &#847; &#8203;</div>
		<table role="presentation" width="600"><tr><td>
			<h2>Invoice Ready</h2>
			<p>Download your receipt from <a href="https://billing.example.org/inv/42">Billing Portal</a>.</p>
		</td></tr></table>
		<table>
			<thead><tr><th>Item</th><th>Amount</th></tr></thead>
			<tbody><tr><td>Plan Pro</td><td>$19.00</td></tr></tbody>
		</table>
	</body></html>`
	plainBody := "Invoice Ready\nDownload your receipt: https://tracker.example.org/click?u=https://billing.example.org/inv/42"

	got := ExtractPreferredBody(htmlBody, plainBody)
	if strings.Contains(got, "Hidden preheader") {
		t.Fatalf("expected hidden preheader to be stripped, got:\n%s", got)
	}
	if !strings.Contains(got, "[Billing Portal](https://billing.example.org/inv/42)") {
		t.Fatalf("expected markdown link from HTML, got:\n%s", got)
	}
	if !strings.Contains(got, "| Item | Amount |") || !strings.Contains(got, "| Plan Pro | $19.00 |") {
		t.Fatalf("expected data table to convert to markdown pipe table, got:\n%s", got)
	}
}

func TestExtractPreferredBodyFallsBackToPlain(t *testing.T) {
	got := ExtractPreferredBody("", "Hello plain world\u200b\n\n\n\nSecond paragraph")
	want := "Hello plain world\n\nSecond paragraph"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHTMLToTextSanitizesImagesAndLinkedBannersWithoutMarkdownImageSyntax(t *testing.T) {
	htmlBody := `<html><body>
		<img src="https://tracker.example.org/open.gif" width="1" height="1" alt="tracker">
		<p><img src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUg==" alt="Universidad de Guadalajara"></p>
		<a href="https://udg.mx/convocatoria_(2026)"><div><img src="https://cdn.udg.mx/banner.png" alt="Ver convocatoria"></div></a>
		<p><a href="https://www.facebook.com/udg.mx"><img src="https://cdn.udg.mx/fb.png" alt=""></a></p>
	</body></html>`

	got := HTMLToText(htmlBody)
	if strings.Contains(got, "![") || strings.Contains(got, "data:image") || strings.Contains(got, "tracker") {
		t.Fatalf("expected zero markdown image syntax or tracking pixels, got:\n%s", got)
	}
	if !strings.Contains(got, "Universidad de Guadalajara") {
		t.Fatalf("expected meaningful image alt text to remain, got:\n%s", got)
	}
	if !strings.Contains(got, "[Ver convocatoria](https://udg.mx/convocatoria_%282026%29)") {
		t.Fatalf("expected linked image banner to become clean markdown link, got:\n%s", got)
	}
	if !strings.Contains(got, "[facebook.com](https://www.facebook.com/udg.mx)") {
		t.Fatalf("expected icon-only link to fall back to domain label, got:\n%s", got)
	}
}

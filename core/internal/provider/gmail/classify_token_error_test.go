package gmail

import (
	"errors"
	"net/http"
	"testing"

	"github.com/arqueon/dankmail/core/errdefs"
	"golang.org/x/oauth2"
)

func TestClassifyPreservesTokenErrorKind(t *testing.T) {
	for _, kind := range []errdefs.Kind{errdefs.KindAuth, errdefs.KindNetwork, errdefs.KindRateLimit} {
		original := errdefs.Wrap(kind, &oauth2.RetrieveError{Response: &http.Response{StatusCode: 400}})
		got := classify(original)
		if errdefs.KindOf(got) != kind || !errors.Is(got, original) {
			t.Fatalf("got %v, want original kind %v", got, kind)
		}
	}
}

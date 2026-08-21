package writes

import (
	"errors"
	"strings"
	"testing"
)

// The dialog that asks a person to confirm money is the last place to paste a URL and a JSON
// body. These are the two halves of that: the refusal is read, and the ones we know are named.
func TestRefusalIsReadAndNamed(t *testing.T) {
	outdated := errors.New(`http 400 for https://fantasy-api.llt-services.com/api/v1/` +
		`competition/1/league/018012894/market/14693409/offer/37547860/cancel?x-lang=es: ` +
		`{"code":400,"message":"Offer has been already outdated","errorCode":"030.01.48"}`)

	got := explain(outdated)
	if !errors.Is(got, ErrOutdated) {
		t.Errorf("una oferta caducada deberia reconocerse como tal: %v", got)
	}
	if strings.Contains(got.Error(), "http 400") || strings.Contains(got.Error(), "llt-services") {
		t.Errorf("el mensaje no deberia arrastrar la peticion: %v", got)
	}

	// An unknown code keeps the API's own sentence, which is more than the transport error says
	// and less than the whole body.
	unknown := errors.New(`http 400 for https://x/y: {"code":400,` +
		`"message":"Team has pending offer in this player","errorCode":"030.01.99"}`)
	if got := explain(unknown).Error(); got != "Team has pending offer in this player" {
		t.Errorf("mensaje %q", got)
	}

	// Nothing to read: the error travels untouched rather than being replaced by a guess.
	plain := errors.New("dial tcp: lookup fantasy-api.llt-services.com: no such host")
	if got := explain(plain); got != plain {
		t.Errorf("un error sin cuerpo deberia pasar tal cual: %v", got)
	}
}

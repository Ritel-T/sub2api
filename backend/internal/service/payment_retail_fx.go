package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	retailFXECBEndpoint     = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"
	retailFXECBNamespace    = "http://www.ecb.int/vocabulary/2002-08-01/eurofxref"
	retailFXEnvelopeNS      = "http://www.gesmes.org/xml/2002-08-01"
	retailFXHTTPTimeout     = 8 * time.Second
	retailFXCacheTTL        = time.Hour
	retailFXFutureTolerance = 5 * time.Minute
	retailFXDefaultMaxAge   = 120
	retailFXMaxAgeLimit     = 240
	retailFXMaxResponseSize = 64 << 10
	retailFXErrorReason     = "PAYMENT_RETAIL_FX_UNAVAILABLE"
)

// time.Parse alone also accepts offset hours of 24 and comma fractions, which
// are outside RFC3339. Require the wire format before parsing its calendar date.
var retailFXRFC3339Pattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d:[0-5]\d(\.\d+)?(Z|[+-]([01]\d|2[0-3]):[0-5]\d)$`)

// RetailFX is a frozen reference for retail pricing: 1 GBP = Rates[currency].
// AsOf records the source timestamp, rather than when it was downloaded.
type RetailFX struct {
	Rates  map[string]float64 `json:"rates"`
	Source string             `json:"source"`
	AsOf   time.Time          `json:"asof"`
}

// The URL is fixed in production. Tests inject an HTTP client and URL through
// a private resolver; administrative settings cannot redirect the fetch.
var defaultRetailFXResolver = &retailFXResolver{
	client: &http.Client{
		Timeout: retailFXHTTPTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	},
	endpoint: retailFXECBEndpoint,
}

type retailFXResolver struct {
	client   *http.Client
	endpoint string

	mu        sync.Mutex
	cached    *RetailFX
	fetchedAt time.Time
}

// ResolveRetailFX resolves validated GBP cross rates for a new retail quote.
// ECB reference data is a pricing input, not an executable currency exchange.
func ResolveRetailFX(ctx context.Context, cfg *PaymentConfig, now time.Time) (*RetailFX, error) {
	return defaultRetailFXResolver.resolve(ctx, cfg, now)
}

func (r *retailFXResolver) resolve(ctx context.Context, cfg *PaymentConfig, now time.Time) (*RetailFX, error) {
	if ctx == nil {
		return nil, retailFXUnavailable("missing request context", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, retailFXUnavailable("request canceled", err)
	}
	if cfg == nil || now.IsZero() {
		return nil, retailFXUnavailable("missing exchange rate configuration or quote time", nil)
	}
	maxAgeHours := cfg.BalanceRetailFXMaxAgeHours
	if maxAgeHours == 0 {
		maxAgeHours = retailFXDefaultMaxAge
	}
	if maxAgeHours < 1 || maxAgeHours > retailFXMaxAgeLimit {
		return nil, retailFXUnavailable("invalid exchange rate maximum age", nil)
	}
	maxAge := time.Duration(maxAgeHours) * time.Hour
	now = now.UTC()

	switch strings.ToLower(strings.TrimSpace(cfg.BalanceRetailFXSource)) {
	case "manual":
		asOfText := strings.TrimSpace(cfg.BalanceRetailFXAsOf)
		if !retailFXRFC3339Pattern.MatchString(asOfText) {
			return nil, retailFXUnavailable("manual rate timestamp must be RFC3339", nil)
		}
		asOf, err := time.Parse(time.RFC3339, asOfText)
		if err != nil {
			return nil, retailFXUnavailable("manual rate timestamp must be RFC3339", err)
		}
		fx := &RetailFX{
			Rates: map[string]float64{
				"GBP": 1,
				"USD": cfg.BalanceRetailFXUSDPerGBP,
				"CNY": cfg.BalanceRetailFXCNYPerGBP,
			},
			Source: "manual",
			AsOf:   asOf.UTC(),
		}
		if err := validateRetailFX(fx, now, maxAge); err != nil {
			return nil, err
		}
		return fx, nil
	case "ecb":
		return r.resolveECB(ctx, now, maxAge)
	default:
		return nil, retailFXUnavailable("exchange rate source must be manual or ecb", nil)
	}
}

func (r *retailFXResolver) resolveECB(ctx context.Context, now time.Time, maxAge time.Duration) (*RetailFX, error) {
	r.mu.Lock()
	cacheAge := now.Sub(r.fetchedAt)
	if r.cached != nil && cacheAge >= 0 && cacheAge < retailFXCacheTTL && validateRetailFX(r.cached, now, maxAge) == nil {
		fx := cloneRetailFX(r.cached)
		r.mu.Unlock()
		return fx, nil
	}
	r.mu.Unlock()

	fx, err := readECBWithClient(ctx, r.client, r.endpoint)
	if err != nil {
		return nil, err
	}
	if err := validateRetailFX(fx, now, maxAge); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.cached = cloneRetailFX(fx)
	r.fetchedAt = now
	r.mu.Unlock()
	return fx, nil
}

func validateRetailFX(fx *RetailFX, now time.Time, maxAge time.Duration) error {
	if fx == nil || fx.AsOf.IsZero() {
		return retailFXUnavailable("missing exchange rate timestamp", nil)
	}
	if fx.AsOf.After(now.Add(retailFXFutureTolerance)) {
		return retailFXUnavailable("exchange rate timestamp is in the future", nil)
	}
	if now.Sub(fx.AsOf) > maxAge {
		return retailFXUnavailable("exchange rate reference has expired", nil)
	}
	if fx.Rates["GBP"] != 1 {
		return retailFXUnavailable("invalid GBP reference rate", nil)
	}
	for _, currency := range []string{"USD", "CNY"} {
		if !retailFXFinitePositive(fx.Rates[currency]) {
			return retailFXUnavailable("missing or invalid "+currency+" reference rate", nil)
		}
	}
	return nil
}

func cloneRetailFX(fx *RetailFX) *RetailFX {
	copy := &RetailFX{Source: fx.Source, AsOf: fx.AsOf, Rates: make(map[string]float64, len(fx.Rates))}
	for currency, rate := range fx.Rates {
		copy.Rates[currency] = rate
	}
	return copy
}

func retailFXFinitePositive(rate float64) bool {
	return rate > 0 && !math.IsNaN(rate) && !math.IsInf(rate, 0)
}

func retailFXUnavailable(message string, cause error) error {
	err := infraerrors.ServiceUnavailable(retailFXErrorReason, message)
	if cause != nil {
		return err.WithCause(cause)
	}
	return err
}

// readECBWithClient permits a test transport without exposing a URL setting.
// Both request lifetime and decoded body size are bounded, including for an
// injected client with no timeout or an automatically decompressed response.
func readECBWithClient(ctx context.Context, client *http.Client, endpoint string) (*RetailFX, error) {
	if client == nil {
		return nil, retailFXUnavailable("ECB HTTP client is unavailable", nil)
	}
	requestCtx, cancel := context.WithTimeout(ctx, retailFXHTTPTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, retailFXUnavailable("cannot build ECB reference request", err)
	}
	request.Header.Set("Accept", "application/xml, text/xml")
	response, err := client.Do(request)
	if err != nil {
		return nil, retailFXUnavailable("ECB reference request failed", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, retailFXUnavailable("ECB reference returned a non-success status", nil)
	}
	if response.ContentLength > retailFXMaxResponseSize {
		return nil, retailFXUnavailable("ECB reference response is too large", nil)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, retailFXMaxResponseSize+1))
	if err != nil {
		return nil, retailFXUnavailable("cannot read ECB reference response", err)
	}
	if len(body) > retailFXMaxResponseSize {
		return nil, retailFXUnavailable("ECB reference response is too large", nil)
	}
	fx, err := parseECBRetailFX(body)
	if err != nil {
		return nil, retailFXUnavailable("invalid ECB reference document", err)
	}
	return fx, nil
}

// parseECBRetailFX accepts only one ECB daily rates cube. Go's standard XML
// decoder never fetches external entities, and directives/DTDs are rejected
// explicitly rather than silently accepted. Ambiguous dates/rates and extra
// roots are rejected before any quote can use them.
func parseECBRetailFX(body []byte) (*RetailFX, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var path []xml.Name
	var asOf time.Time
	rates := make(map[string]float64)
	rootSeen, rootClosed, declarationSeen := false, false, false
	containerCount, dateCount := 0, 0

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch token := token.(type) {
		case xml.Directive:
			return nil, fmt.Errorf("XML directives are not permitted")
		case xml.ProcInst:
			if token.Target != "xml" || declarationSeen || rootSeen {
				return nil, fmt.Errorf("unexpected XML processing instruction")
			}
			declarationSeen = true
		case xml.StartElement:
			if rootClosed || len(path) >= 16 {
				return nil, fmt.Errorf("extra root or excessive XML depth")
			}
			path = append(path, token.Name)
			if len(path) == 1 {
				if rootSeen || token.Name.Local != "Envelope" || token.Name.Space != retailFXEnvelopeNS {
					return nil, fmt.Errorf("unexpected ECB document root")
				}
				rootSeen = true
				continue
			}
			inRates := len(path) >= 3 && path[1].Local == "Cube"
			if token.Name.Local != "Cube" {
				if inRates {
					return nil, fmt.Errorf("unexpected element inside ECB rates")
				}
				continue
			}
			if token.Name.Space != retailFXECBNamespace {
				return nil, fmt.Errorf("unexpected ECB cube namespace")
			}
			switch len(path) {
			case 2:
				containerCount++
				if containerCount != 1 || len(token.Attr) != 0 {
					return nil, fmt.Errorf("unexpected ECB rates container")
				}
			case 3:
				dateCount++
				if !inRates || dateCount != 1 || len(token.Attr) != 1 || token.Attr[0].Name.Local != "time" || token.Attr[0].Name.Space != "" {
					return nil, fmt.Errorf("missing or ambiguous ECB reference date")
				}
				asOf, err = time.Parse("2006-01-02", token.Attr[0].Value)
				if err != nil || asOf.IsZero() {
					return nil, fmt.Errorf("invalid ECB reference date")
				}
			case 4:
				if !inRates || path[2].Local != "Cube" || dateCount != 1 || len(token.Attr) != 2 {
					return nil, fmt.Errorf("unexpected ECB currency entry")
				}
				var currency, value string
				for _, attr := range token.Attr {
					if attr.Name.Space != "" {
						return nil, fmt.Errorf("unexpected ECB rate attribute namespace")
					}
					switch attr.Name.Local {
					case "currency":
						if currency != "" {
							return nil, fmt.Errorf("duplicate ECB currency attribute")
						}
						currency = attr.Value
					case "rate":
						if value != "" {
							return nil, fmt.Errorf("duplicate ECB rate attribute")
						}
						value = attr.Value
					default:
						return nil, fmt.Errorf("unexpected ECB rate attribute")
					}
				}
				if !retailFXCurrencyCode(currency) {
					return nil, fmt.Errorf("invalid ECB currency code")
				}
				if _, exists := rates[currency]; exists {
					return nil, fmt.Errorf("duplicate ECB currency entry")
				}
				rate, parseErr := strconv.ParseFloat(value, 64)
				if parseErr != nil || !retailFXFinitePositive(rate) {
					return nil, fmt.Errorf("invalid ECB currency rate")
				}
				rates[currency] = rate
			default:
				return nil, fmt.Errorf("unexpected ECB cube depth")
			}
		case xml.EndElement:
			path = path[:len(path)-1]
			if len(path) == 0 {
				rootClosed = true
			}
		case xml.CharData:
			if (len(path) == 0 || (len(path) >= 2 && path[1].Local == "Cube")) && len(bytes.TrimSpace(token)) > 0 {
				return nil, fmt.Errorf("unexpected text in ECB rates document")
			}
		}
	}
	if !rootSeen || !rootClosed || containerCount != 1 || dateCount != 1 {
		return nil, fmt.Errorf("incomplete ECB reference document")
	}
	for _, currency := range []string{"GBP", "USD", "CNY"} {
		if !retailFXFinitePositive(rates[currency]) {
			return nil, fmt.Errorf("missing required ECB currency")
		}
	}
	fx := &RetailFX{
		Rates: map[string]float64{
			"GBP": 1,
			"USD": rates["USD"] / rates["GBP"],
			"CNY": rates["CNY"] / rates["GBP"],
		},
		Source: "ecb",
		AsOf:   asOf.UTC(),
	}
	if !retailFXFinitePositive(fx.Rates["USD"]) || !retailFXFinitePositive(fx.Rates["CNY"]) {
		return nil, fmt.Errorf("invalid ECB GBP cross rate")
	}
	return fx, nil
}

func retailFXCurrencyCode(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for i := range currency {
		if currency[i] < 'A' || currency[i] > 'Z' {
			return false
		}
	}
	return true
}

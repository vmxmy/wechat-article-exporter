package processor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

type PayloadVariant string

const (
	PayloadCGIDataNew   PayloadVariant = "window.cgiDataNew"
	PayloadCGIData      PayloadVariant = "window.cgiData"
	PayloadEmbeddedJSON PayloadVariant = "embedded_json"
)

type Result struct {
	Classification Classification `json:"classification"`
	PayloadVariant PayloadVariant `json:"payloadVariant,omitempty"`
	Article        *Article       `json:"article,omitempty"`
	Resources      []Resource     `json:"resources,omitempty"`
}

type Processor interface {
	Process(context.Context, io.Reader) (Result, error)
}

type Options struct {
	Limits   Limits
	Location *time.Location
}

type HTMLProcessor struct {
	limits   Limits
	location *time.Location
}

func New(options ...Options) *HTMLProcessor {
	var option Options
	if len(options) > 0 {
		option = options[0]
	}
	location := option.Location
	if location == nil {
		location, _ = time.LoadLocation("Asia/Shanghai")
		if location == nil {
			location = time.FixedZone("CST", 8*60*60)
		}
	}
	return &HTMLProcessor{limits: option.Limits.withDefaults(), location: location}
}

func (processor *HTMLProcessor) Process(ctx context.Context, reader io.Reader) (Result, error) {
	if reader == nil {
		return processor.parseFailure(ReasonMalformedPayload, processError(ErrorMalformed, ReasonMalformedPayload, 0, "nil input reader"))
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	limited := io.LimitReader(reader, processor.limits.MaxInputBytes+1)
	html, err := io.ReadAll(limited)
	if err != nil {
		return processor.parseFailure(ReasonMalformedPayload, fmt.Errorf("read article HTML: %w", err))
	}
	if int64(len(html)) > processor.limits.MaxInputBytes {
		return processor.parseFailure(ReasonInputLimit, processError(ErrorLimit, ReasonInputLimit, len(html), "input exceeds %d bytes", processor.limits.MaxInputBytes))
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	if classification, ok := classifyKnownPage(html); ok {
		return Result{Classification: classification}, nil
	}

	hasContentRoot := containsArticleRoot(html)
	payload, variant, extractErr := extractPayload(html, processor.limits)
	if extractErr != nil {
		var typed *ProcessError
		if errors.As(extractErr, &typed) {
			return processor.parseFailure(typed.Reason, extractErr)
		}
		return processor.parseFailure(ReasonMalformedPayload, extractErr)
	}
	if payload == nil {
		return processor.parseFailure(ReasonMissingPayload, processError(ErrorUnsupported, ReasonMissingPayload, 0, "no supported WeChat CGI payload found"))
	}
	if !hasContentRoot {
		return processor.parseFailure(ReasonMissingContentRoot, processError(ErrorInvalid, ReasonMissingContentRoot, 0, "article content root #js_article is missing"))
	}

	article, normalizeErr := normalizeArticle(payload, processor.location)
	if normalizeErr != nil {
		return processor.parseFailure(ReasonInvalidArticle, normalizeErr)
	}
	resources, resourceErr := DiscoverResources(article.Content, article.Media, processor.limits)
	if resourceErr != nil {
		var typed *ProcessError
		if errors.As(resourceErr, &typed) {
			return processor.parseFailure(typed.Reason, resourceErr)
		}
		return processor.parseFailure(ReasonMalformedPayload, resourceErr)
	}

	return Result{
		Classification: Classification{State: ClassificationValid},
		PayloadVariant: variant,
		Article:        &article,
		Resources:      resources,
	}, nil
}

func (processor *HTMLProcessor) parseFailure(reason ReasonCode, err error) (Result, error) {
	message := "article payload could not be parsed"
	if err != nil {
		message = err.Error()
	}
	return Result{Classification: Classification{State: ClassificationParseError, Reason: reason, Message: message}}, err
}

func containsArticleRoot(html []byte) bool {
	lower := bytes.ToLower(html)
	for _, pattern := range [][]byte{
		[]byte(`id="js_article"`),
		[]byte(`id='js_article'`),
		[]byte(`id = "js_article"`),
		[]byte(`id = 'js_article'`),
	} {
		if bytes.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

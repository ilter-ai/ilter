package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/ilter-ai/ilter/internal/features/loopdetect"
	"github.com/ilter-ai/ilter/internal/features/pii"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
	"github.com/ilter-ai/ilter/internal/provider"
)

func writeFlushedChunk(w io.Writer, content, modelID string) {
	if content == "" {
		return
	}
	chunk := &model.ChatCompletionChunk{
		ID:      "chatcmpl-flush",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   modelID,
		Choices: []model.ChunkChoice{
			{Index: 0, Delta: model.Delta{Content: content}},
		},
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		slog.Debug("write chunk error", "error", err)
	}
}

func processStreamChunk(chunk *model.ChatCompletionChunk, unmasker *pii.StreamUnmasker, accumulated *string, finalUsage **model.Usage) {
	if chunk.Usage != nil {
		*finalUsage = chunk.Usage
	}
	for i := range chunk.Choices {
		unmasked := unmasker.Process(chunk.Choices[i].Delta.Content)
		chunk.Choices[i].Delta.Content = unmasked
		*accumulated += unmasked
	}
}

func writeSSEData(w io.Writer, data []byte) bool {
	if _, err := w.Write([]byte("data: ")); err != nil {
		return false
	}
	if _, err := w.Write(data); err != nil {
		return false
	}
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return false
	}
	return true
}

func writeStreamDone(w io.Writer, flusher http.Flusher) {
	if _, err := fmt.Fprintf(w, "data: [DONE]\n\n"); err != nil {
		slog.Debug("write done error", "error", err)
	}
	flusher.Flush()
}

// handleOutputLoop feeds the output detector with delta content from chunk choices.
// Returns true if an output loop was detected and handled (stream termination sent).
func handleOutputLoop(
	choices []model.ChunkChoice,
	detector *loopdetect.OutputDetector,
	cancel context.CancelFunc,
	w io.Writer,
	flusher http.Flusher,
	requestedModel string,
) bool {
	for i := range choices {
		delta := choices[i].Delta.Content
		if delta == "" {
			continue
		}
		result := detector.Feed(delta)
		if !result.Detected {
			continue
		}

		slog.Warn(
			"Output loop detected",
			"repeat_count", result.RepeatCount,
			"sentence", result.Sentence,
			"mode", result.Mode,
		)

		if result.Mode == "enforce" {
			// Cancel upstream provider to stop token billing
			cancel()

			// Send a chunk with the custom finish_reason so clients can differentiate
			reason := "loop_detected"
			loopTermination := model.ChatCompletionChunk{
				ID:      "chatcmpl-output-loop",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   requestedModel,
				Choices: []model.ChunkChoice{
					{
						Index:        0,
						Delta:        model.Delta{Content: ""},
						FinishReason: &reason,
					},
				},
			}
			loopBytes, err := json.Marshal(loopTermination)
			if err == nil {
				writeSSEData(w, loopBytes)
			}
			writeStreamDone(w, flusher)
			return true
		}

		// Observe mode: log only, continue streaming
		return false
	}
	return false
}

func (h *Handler) handleStreaming(
	ctx context.Context,
	cancel context.CancelFunc,
	w http.ResponseWriter,
	resp *http.Response,
	p provider.Provider,
	route *smartrouter.Route,
	requestedModel string,
	start time.Time,
	originalRequest *http.Request,
	req *model.ChatCompletionRequest,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "streaming unsupported")
		var msgs []model.Message
		if req != nil {
			msgs = req.Messages
		}
		h.recordAudit(originalRequest, route, requestedModel, nil, http.StatusInternalServerError, start, false, msgs)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	actualModel := route.Model.Name
	if route.Provider != nil {
		actualModel = route.Provider.Name() + "/" + route.Model.Name
	}
	w.Header().Set("X-Ilter-Model-Actual", actualModel)
	flusher.Flush()

	defer resp.Body.Close()

	var accumulatedCompletionText string
	var finalUsage *model.Usage
	statusCode := http.StatusOK

	defer func() {
		pTokens := 0
		cTokens := 0
		if finalUsage != nil {
			pTokens = finalUsage.PromptTokens
			cTokens = finalUsage.CompletionTokens
		} else if req != nil {
			charCount := 0
			for _, m := range req.Messages {
				if sContent, ok := m.Content.(string); ok {
					charCount += len(sContent)
				}
			}
			pTokens = charCount / 4
			if pTokens < 1 {
				pTokens = 1
			}
			cTokens = len(accumulatedCompletionText) / 4
		}

		auditResp := &model.ChatCompletionResponse{
			Usage: &model.Usage{
				PromptTokens:     pTokens,
				CompletionTokens: cTokens,
				TotalTokens:      pTokens + cTokens,
			},
			Choices: []model.Choice{
				{
					Message: model.ChoiceMessage{
						Role:    "assistant",
						Content: accumulatedCompletionText,
					},
				},
			},
		}

		var msgs []model.Message
		if req != nil {
			msgs = req.Messages
		}
		h.recordAudit(originalRequest, route, requestedModel, auditResp, statusCode, start, false, msgs)

		keyID := reqmeta.GetKeyID(originalRequest.Context())
		cost := CalculateCost(route.Model, pTokens, cTokens)

		meta := reqmeta.GetRequestMetadata(originalRequest.Context())
		if meta != nil {
			meta.SetTokensAndCost(pTokens, cTokens, cost)
		}

		if h.budgetEnforcer != nil {
			if err := h.budgetEnforcer.RecordUsage(originalRequest.Context(), keyID, cost); err != nil {
				slog.Error("failed to record budget usage for stream", "error", err)
			}
		}
		if h.loopDetector != nil {
			h.loopDetector.RecordCost(keyID, cost)
		}
	}()

	mappings := middleware.GetPIIMappings(ctx)
	unmasker := pii.NewStreamUnmasker(mappings)

	// Output loop detector (nil when loop detection is disabled)
	var outputDetector *loopdetect.OutputDetector
	if h.cfg != nil && h.cfg.CostGuard.LoopSettings.OutputLoopMode != "off" {
		outputDetector = loopdetect.NewOutputDetector(
			h.cfg.CostGuard.LoopSettings.OutputLoopThreshold,
			h.cfg.CostGuard.LoopSettings.OutputMinSentence,
			h.cfg.CostGuard.LoopSettings.OutputLoopMode,
		)
	}

	streamProvider, _ := p.(provider.StreamingProvider)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		select {
		case <-ctx.Done():
			slog.Debug("client disconnected during stream")
			return
		default:
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		if bytes.HasPrefix(line, []byte(":")) || bytes.HasPrefix(line, []byte("event:")) {
			if _, err := w.Write(line); err != nil {
				slog.Debug("forward line error", "error", err)
			}
			if _, err := w.Write([]byte("\n")); err != nil {
				slog.Debug("forward newline error", "error", err)
			}
			flusher.Flush()
			continue
		}

		var dataBytes []byte
		if bytes.HasPrefix(line, []byte("data: ")) {
			dataBytes = bytes.TrimPrefix(line, []byte("data: "))
		} else {
			dataBytes = line
		}

		if streamProvider != nil {
			chunk, done, err := streamProvider.TransformStreamChunk(dataBytes)
			if err != nil {
				slog.Error("Error transforming stream chunk", "error", err)
				continue
			}
			if done {
				writeFlushedChunk(w, unmasker.Flush(), requestedModel)
				writeStreamDone(w, flusher)
				return
			}
			if chunk != nil {
				processStreamChunk(chunk, unmasker, &accumulatedCompletionText, &finalUsage)

				if chunk.Usage != nil {
					cost := CalculateCost(route.Model, chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens)
					chunk.Usage.IlterCost = cost
					if id, ok := ctx.Value(reqmeta.KeyIDContextKey).(string); ok && id != "" {
						chunk.Usage.IlterBillingKey = id
					}
				}

				// Output loop detection
				if outputDetector != nil && cancel != nil && handleOutputLoop(chunk.Choices, outputDetector, cancel, w, flusher, requestedModel) {
					return
				}

				chunkBytes, err := json.Marshal(chunk)
				if err == nil {
					if !writeSSEData(w, chunkBytes) {
						return
					}
					flusher.Flush()
				}
			}
		} else {
			if bytes.Equal(dataBytes, []byte("[DONE]")) {
				writeFlushedChunk(w, unmasker.Flush(), requestedModel)
				writeStreamDone(w, flusher)
				return
			}

			var rawChunk model.ChatCompletionChunk
			if errJSON := json.Unmarshal(dataBytes, &rawChunk); errJSON == nil {
				processStreamChunk(&rawChunk, unmasker, &accumulatedCompletionText, &finalUsage)

				if rawChunk.Usage != nil {
					cost := CalculateCost(route.Model, rawChunk.Usage.PromptTokens, rawChunk.Usage.CompletionTokens)
					rawChunk.Usage.IlterCost = cost
					if id, ok := ctx.Value(reqmeta.KeyIDContextKey).(string); ok && id != "" {
						rawChunk.Usage.IlterBillingKey = id
					}
				}

				// Output loop detection
				if outputDetector != nil && cancel != nil && handleOutputLoop(rawChunk.Choices, outputDetector, cancel, w, flusher, requestedModel) {
					return
				}

				if newDataBytes, err := json.Marshal(rawChunk); err == nil {
					dataBytes = newDataBytes
				}
			}

			if !writeSSEData(w, dataBytes) {
				return
			}
			flusher.Flush()
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Error("stream scanner error", "error", err)
	}

	writeFlushedChunk(w, unmasker.Flush(), requestedModel)
}

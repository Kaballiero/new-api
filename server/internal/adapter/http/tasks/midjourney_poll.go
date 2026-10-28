package tasks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/internal/tasks/poll_midjourney_tasks"
	"github.com/QuantumNous/new-api/logger"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
)

// MidjourneyPollClient preserves the grouped legacy query and its timeout.
type MidjourneyPollClient struct{}

func (MidjourneyPollClient) Fetch(ctx context.Context, channel poll_midjourney_tasks.Channel, taskIDs []string) ([]poll_midjourney_tasks.Result, error) {
	requestURL := fmt.Sprintf("%s/mj/task/list-by-condition", channel.BaseURL)
	body, err := common.Marshal(map[string]any{"ids": taskIDs})
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Task marshal body error: %v", err))
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	req, err := http.NewRequestWithContext(requestCtx, "POST", requestURL, bytes.NewBuffer(body))
	if err != nil {
		cancel()
		logger.LogError(ctx, fmt.Sprintf("Get Task error: %v", err))
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("mj-api-secret", channel.Key)
	resp, err := httpclient.GetHttpClient().Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Task Do req error: %v", err))
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("Midjourney task HTTP status %d", resp.StatusCode)
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Mjp Task parse body error: %v", err))
		resp.Body.Close()
		cancel()
		return nil, err
	}
	var items []dto.MidjourneyDto
	if err = common.Unmarshal(responseBody, &items); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Mjp Task parse body error2: %v, body: %s", err, string(responseBody)))
		resp.Body.Close()
		cancel()
		return nil, err
	}
	resp.Body.Close()
	req.Body.Close()
	cancel()
	if items == nil {
		return nil, nil
	}
	results := make([]poll_midjourney_tasks.Result, len(items))
	for i := range items {
		item := items[i]
		result := poll_midjourney_tasks.Result{MjId: item.MjId, Progress: item.Progress, PromptEn: item.PromptEn,
			State: item.State, SubmitTime: item.SubmitTime, StartTime: item.StartTime, FinishTime: item.FinishTime,
			ImageUrl: item.ImageUrl, Status: item.Status, FailReason: item.FailReason, Buttons: item.Buttons, VideoUrl: item.VideoUrl}
		if item.Properties != nil {
			result.Properties = item.Properties
		}
		if item.VideoUrls != nil {
			result.VideoUrls = make([]poll_midjourney_tasks.VideoURL, len(item.VideoUrls))
			for j := range item.VideoUrls {
				result.VideoUrls[j] = poll_midjourney_tasks.VideoURL{Url: item.VideoUrls[j].Url}
			}
		}
		results[i] = result
	}
	return results, nil
}

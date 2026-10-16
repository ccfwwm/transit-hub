package channel_monitor

import (
	"context"
	"fmt"
	"log"
	"time"

	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

type DurableMediaStore interface {
	MediaTaskStore
	ClaimMediaTask(context.Context, MediaTask, time.Time) (bool, error)
	ActiveMediaTask(context.Context, string) (*MediaTask, error)
}

type completedMediaTaskContextKey struct{}

func (s *Service) mediaCheck(ctx context.Context, rule Rule, conn my_sites.RealConnection, site upstream.Site, target TargetState, o AccountTestOptions) (AccountTestResult, error) {
	store, ok := s.store.(DurableMediaStore)
	if !ok {
		return AccountTestResult{}, fmt.Errorf("媒体任务持久化不可用，未执行生成")
	}
	key := rule.ID + "|" + target.Fingerprint
	task, err := store.GetMediaTask(ctx, key)
	if err != nil {
		return AccountTestResult{}, err
	}
	if task != nil && (task.Status == "success" || task.Status == "failed") {
		// Consume a completed task before considering a new check. Manual double
		// clicks within the check interval also reuse its terminal result.
		if target.CheckedAt == nil || target.CheckedAt.Before(task.UpdatedAt) || time.Since(task.UpdatedAt) < time.Duration(rule.CheckIntervalMinutes)*time.Minute {
			out := taskResult(*task)
			if target.CheckedAt != nil && !target.CheckedAt.Before(task.UpdatedAt) {
				out.Reused = true
			}
			return out, nil
		}
	}
	if task == nil || task.Status == "success" || task.Status == "failed" {
		active, err := store.ActiveMediaTask(ctx, rule.ID)
		if err != nil {
			return AccountTestResult{}, err
		}
		if active != nil && active.Key != key {
			if active.Status == "pending" && time.Since(active.UpdatedAt) >= 10*time.Second && active.Options.ModelID != "" {
				s.queueMedia(active.Key, func() { s.executeMedia(store, *active, conn, site, active.Options, true) })
			}
			return AccountTestResult{Pending: true, Message: "上一次媒体任务尚未结束或待确认；配置变更后暂不提交新生成", Model: o.ModelID, TaskID: active.TaskID, MediaTaskKey: active.Key}, nil
		}
		newTask := MediaTask{Key: key, UserID: rule.UserID, AdminAccountID: rule.AdminAccountID, RuleID: rule.ID, GroupID: target.GroupID, ModelID: o.ModelID, Mode: o.Mode, ConfigFingerprint: target.Fingerprint, Status: "submitting"}
		newTask.Options = o
		claimed, err := store.ClaimMediaTask(ctx, newTask, time.Now().Add(-time.Duration(rule.CheckIntervalMinutes)*time.Minute))
		if err != nil {
			return AccountTestResult{}, err
		}
		if claimed {
			task, err = store.GetMediaTask(ctx, key)
			if err != nil {
				return AccountTestResult{}, err
			}
			s.queueMedia(key, func() { s.executeMedia(store, *task, conn, site, o, false) })
			return AccountTestResult{Pending: true, Message: "媒体生成已排队，后台执行", Model: o.ModelID}, nil
		}
		task, err = store.GetMediaTask(ctx, key)
		if err != nil {
			return AccountTestResult{}, err
		}
	}
	if task == nil {
		return AccountTestResult{}, fmt.Errorf("媒体任务登记失败，未执行生成")
	}
	if task.Status == "pending" && time.Since(task.UpdatedAt) >= 10*time.Second {
		if time.Since(task.CreatedAt) >= 60*time.Minute {
			task.Status = "timeout"
			task.Message = "已超过 60 分钟观察上限；未提交新任务，请核对上游后重置"
			task.UpdatedAt = time.Now()
			if err = store.SaveMediaTask(ctx, *task); err != nil {
				return AccountTestResult{}, err
			}
		} else {
			s.queueMedia(key, func() { s.executeMedia(store, *task, conn, site, o, true) })
		}
	}
	return taskResult(*task), nil
}

func taskResult(task MediaTask) AccountTestResult {
	out := AccountTestResult{Model: task.ModelID, TaskID: task.TaskID, Message: task.Message, MediaTaskKey: task.Key}
	switch task.Status {
	case "success":
		out.Success = true
	case "failed":
	case "unknown", "timeout":
		out.Uncertain = true
	default:
		out.Pending = true
		if out.Message == "" {
			out.Message = "媒体任务生成中；受理不明确时需显式重置"
		}
	}
	return out
}

func (s *Service) queueMedia(key string, work func()) {
	if _, loaded := s.mediaActive.LoadOrStore(key, true); loaded {
		return
	}
	s.mediaWorkers.Do(func() {
		for i := 0; i < 2; i++ {
			go func() {
				for {
					s.mediaQueueMu.Lock()
					for len(s.mediaQueue) == 0 {
						s.mediaQueueCond.Wait()
					}
					job := s.mediaQueue[0]
					s.mediaQueue[0] = nil
					s.mediaQueue = s.mediaQueue[1:]
					s.mediaQueueMu.Unlock()
					job()
				}
			}()
		}
	})
	s.mediaQueueMu.Lock()
	s.mediaQueue = append(s.mediaQueue, func() { defer s.mediaActive.Delete(key); work() })
	s.mediaQueueCond.Signal()
	s.mediaQueueMu.Unlock()
}

func (s *Service) executeMedia(store DurableMediaStore, task MediaTask, conn my_sites.RealConnection, site upstream.Site, o AccountTestOptions, poll bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if poll {
		o.TaskID = task.TaskID
	}
	result, err := s.platform.ProbeUpstreamConnection(ctx, site, conn, o)
	if result.TaskID != "" {
		task.TaskID = result.TaskID
	}
	switch {
	case err != nil && poll:
		task.Status = "pending"
		task.Message = "上游轮询暂不可用，将继续查询原任务"
	case err != nil:
		task.Status = "unknown"
		task.Message = "提交异常，受理结果待确认；不会自动重新提交"
	case result.Uncertain:
		task.Status = "unknown"
		task.Message = result.Message
	case result.Pending:
		task.Status = "pending"
		task.Message = result.Message
	case result.Success:
		task.Status = "success"
		task.Message = result.Message
	default:
		task.Status = "failed"
		task.Message = result.Message
	}
	task.UpdatedAt = time.Now()
	// A reset cannot race an active worker and resurrect its old task state.
	if err = store.SaveMediaTask(ctx, task); err != nil {
		log.Printf("[channel-monitor] persist media outcome failed rule_id=%s", task.RuleID)
		return
	}
	if task.Status == "success" || task.Status == "failed" {
		s.completeMediaTask(task)
	}
}

// Finish manually started checks even when periodic monitoring is disabled.
// The normal account lock and fingerprint check prevent a late result from
// replacing a newer configuration or consuming the same outcome twice.
func (s *Service) completeMediaTask(task MediaTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rule, err := s.store.GetRule(ctx, task.RuleID)
	if err != nil || rule == nil {
		return
	}
	ctx = context.WithValue(ctx, completedMediaTaskContextKey{}, task.Key)
	if _, err = s.runRule(ctx, *rule, "media_complete"); err != nil {
		log.Printf("[channel-monitor] consume media outcome failed rule_id=%s", task.RuleID)
	}
}

// Recovery is independent of the rule's enabled state and current model config.
// A previously accepted generation is polled with its original parameters.
func (s *Service) recoverMediaTasks(ctx context.Context) {
	reader, ok := s.store.(interface {
		ListPendingMediaTasks(context.Context) ([]MediaTask, error)
	})
	store, durable := s.store.(DurableMediaStore)
	if !ok || !durable {
		return
	}
	tasks, err := reader.ListPendingMediaTasks(ctx)
	if err != nil {
		return
	}
	for _, task := range tasks {
		if time.Since(task.CreatedAt) >= 60*time.Minute {
			task.Status = "timeout"
			task.Message = "已超过 60 分钟观察上限；未提交新任务，请核对上游后重置"
			_ = store.SaveMediaTask(ctx, task)
			continue
		}
		conn, e := s.conns.GetRealConnection(ctx, task.RuleID, task.UserID, task.AdminAccountID)
		if e != nil || conn == nil || task.Options.ModelID == "" {
			continue
		}
		// Credentials/site changes require reconciliation; do not query the old
		// task under a new identity.
		if configFingerprint(*conn, task.Options) != task.ConfigFingerprint {
			continue
		}
		site, e := s.upstreams.GetSite(ctx, conn.UpstreamSiteID)
		if e != nil || site == nil {
			continue
		}
		s.queueMedia(task.Key, func() { s.executeMedia(store, task, *conn, *site, task.Options, true) })
	}
}

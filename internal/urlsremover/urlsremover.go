package urlsremover

import (
	"context"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"time"
)

type userUrlsKeeper interface {
	RemoveUsersUrls(
		ctx context.Context,
		usersURLs map[string][]string,
	) error
}

type task struct {
	userID      string
	urlToDelete string
}

type UrlsRemover struct {
	queue                    chan *task
	db                       userUrlsKeeper
	delayBetweenQueueFetches time.Duration
	errorChannel             chan error
}

func (r *UrlsRemover) ListenErrors(callback func(error)) {
	go func() {
		for err := range r.errorChannel {
			callback(err)
		}
	}()
}

func (r *UrlsRemover) collectUrlsByUser(tasks []task) map[string][]string {
	result := map[string][]string{}
	for _, t := range tasks {
		_, ok := result[t.userID]
		if !ok {
			result[t.userID] = []string{}
		}
		result[t.userID] = append(result[t.userID], t.urlToDelete)
	}

	return result
}

func (r *UrlsRemover) Run(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(r.delayBetweenQueueFetches)
		defer ticker.Stop()

		var tasks []task

		for {
			select {
			case <-ctx.Done():
				logger.Log.Infoln("UrlsRemover.Run() stopped")
				return
			case t := <-r.queue:
				tasks = append(tasks, *t)
			case <-ticker.C:
				if len(tasks) == 0 {
					continue
				}
				err := r.db.RemoveUsersUrls(context.TODO(), r.collectUrlsByUser(tasks))
				if err != nil {
					r.errorChannel <- err
					continue
				}
				logger.Log.Infof("processed removing of %d URLs", len(tasks))
				tasks = nil
			}
		}
	}()
}

func New(
	db userUrlsKeeper,
	channelCapacity int,
	delayBetweenQueueFetches time.Duration,
) *UrlsRemover {
	return &UrlsRemover{
		db:                       db,
		queue:                    make(chan *task, channelCapacity),
		delayBetweenQueueFetches: delayBetweenQueueFetches,
		errorChannel:             make(chan error, channelCapacity),
	}
}

func (r *UrlsRemover) EnqueueJob(job *models.URLDeleteJob) {
	for _, URLId := range job.URLsToDelete {
		r.queue <- &task{
			userID:      job.UserID,
			urlToDelete: URLId,
		}
	}
}

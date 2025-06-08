package urlsremover

import (
	"context"
	"time"

	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
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

// URLsRemover is responsible for managing and executing background URL deletion jobs.
// It maintains an internal job queue and processes deletion tasks asynchronously.
type URLsRemover struct {
	queue                    chan *task
	db                       userUrlsKeeper
	delayBetweenQueueFetches time.Duration
	errorChannel             chan error
}

// New initializes and returns a new instance of URLsRemover.
func New(
	db userUrlsKeeper,
	channelCapacity int,
	delayBetweenQueueFetches time.Duration,
) *URLsRemover {
	return &URLsRemover{
		db:                       db,
		queue:                    make(chan *task, channelCapacity),
		delayBetweenQueueFetches: delayBetweenQueueFetches,
		errorChannel:             make(chan error, channelCapacity),
	}
}

// ListenErrors starts a goroutine that listens for errors from the internal
// error channel and passes them to the provided callback function.
//
// The callback is invoked for each error as it arrives. This method returns immediately,
// and the listening continues in the background.
func (r *URLsRemover) ListenErrors(callback func(error)) {
	go func() {
		for err := range r.errorChannel {
			callback(err)
		}
	}()
}

// Run starts a background goroutine that periodically processes queued URL deletion jobs.
// The method returns immediately and continues processing in the background until the provided context is canceled.
func (r *URLsRemover) Run(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(r.delayBetweenQueueFetches)
		defer ticker.Stop()

		var tasks []task

		for {
			select {
			case <-ctx.Done():
				logger.Log.Infoln("URLsRemover.Run() stopped")
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

// EnqueueJob adds a new URLDeleteJob to the background processing queue.
func (r *URLsRemover) EnqueueJob(job *models.URLDeleteJob) {
	for _, URLId := range job.URLsToDelete {
		r.queue <- &task{
			userID:      job.UserID,
			urlToDelete: URLId,
		}
	}
}

func (r *URLsRemover) collectUrlsByUser(tasks []task) map[string][]string {
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

package urlsremover

import (
	"context"
	"github.com/patric-chuzhbe/urlshrt/internal/db/storage"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/urlsremoverinterface"
	"time"
)

type task struct {
	userID      int
	urlToDelete string
}

type UrlsRemover struct {
	queue                    chan *task
	db                       storage.Storage
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

func (r *UrlsRemover) collectUrlsByUser(tasks []task) map[int][]string {
	result := map[int][]string{}
	for _, t := range tasks {
		_, ok := result[t.userID]
		if !ok {
			result[t.userID] = []string{}
		}
		result[t.userID] = append(result[t.userID], t.urlToDelete)
	}

	return result
}

func (r *UrlsRemover) Run() {
	go func() {
		ticker := time.NewTicker(r.delayBetweenQueueFetches * time.Second)

		var tasks []task

		for {
			select {
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
	db storage.Storage,
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

func (r *UrlsRemover) EnqueueJob(job *urlsremoverinterface.Job) {
	for _, URLId := range job.URLsToDelete {
		r.queue <- &task{
			userID:      job.UserID,
			urlToDelete: URLId,
		}
	}
}

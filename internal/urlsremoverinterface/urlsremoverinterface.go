package urlsremoverinterface

import "github.com/patric-chuzhbe/urlshrt/internal/models"

type Job struct {
	UserID       int
	URLsToDelete models.DeleteURLsRequest
}

type URLsRemoverInterface interface {
	EnqueueJob(job *Job)
	Run()
}

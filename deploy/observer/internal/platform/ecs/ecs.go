// Package ecs adapts the Amazon ECS control plane to platform.Lister
// (docs/design/deployment-transparency/README.md, "Run, freshness, and
// staleness"; docs/design/deployment-transparency/document.md, "Component
// mapping"). It calls only ecs:ListTasks and ecs:DescribeTasks, and returns
// platform.Image values built from nothing but a container's name, image
// digest, last status, and start time.
package ecs

import (
	"context"
	"fmt"

	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
)

// maxDescribeTasks is the largest task list ecs:DescribeTasks accepts in one
// call.
const maxDescribeTasks = 100

// role names the configured service a task came from, independent of the
// task's own group string, which the adapter never reads.
type role string

const (
	roleApp         role = "app"
	roleWeb         role = "web"
	roleMaintenance role = "maintenance"
)

// Config names the ECS cluster and the three serving services
// (docs/design/deployment-transparency/README.md, "Run, freshness, and
// staleness").
type Config struct {
	Cluster            string
	AppService         string
	WebService         string
	MaintenanceService string
}

// Lister lists the running images of the configured cluster's serving
// services. It implements platform.Lister.
type Lister struct {
	client *awsecs.Client
	cfg    Config
}

// New returns a Lister that calls client against cfg's cluster and services.
func New(client *awsecs.Client, cfg Config) *Lister {
	return &Lister{client: client, cfg: cfg}
}

// Running implements platform.Lister. It calls ListTasks once per serving
// service and DescribeTasks once for every task returned, then aggregates
// the RUNNING containers into platform.Image values.
func (l *Lister) Running(ctx context.Context) ([]platform.Image, error) {
	var arns []string
	roleByARN := map[string]role{}
	for _, svc := range []struct {
		name string
		role role
	}{
		{l.cfg.AppService, roleApp},
		{l.cfg.WebService, roleWeb},
		{l.cfg.MaintenanceService, roleMaintenance},
	} {
		found, err := l.listRunningTaskARNs(ctx, svc.name)
		if err != nil {
			return nil, fmt.Errorf("list tasks for %s: %w", svc.role, err)
		}
		for _, arn := range found {
			roleByARN[arn] = svc.role
		}
		arns = append(arns, found...)
	}
	if len(arns) == 0 {
		return platform.Aggregate(nil)
	}
	if len(arns) > maxDescribeTasks {
		return nil, fmt.Errorf("cluster runs %d tasks, more than the %d DescribeTasks accepts in one call", len(arns), maxDescribeTasks)
	}
	out, err := l.client.DescribeTasks(ctx, &awsecs.DescribeTasksInput{
		Cluster: &l.cfg.Cluster,
		Tasks:   arns,
	})
	if err != nil {
		return nil, fmt.Errorf("describe tasks: %w", err)
	}
	// A task ListTasks returned but DescribeTasks could not describe would
	// silently drop out of the document; treat it as a failed read.
	if len(out.Failures) > 0 {
		return nil, fmt.Errorf("describe tasks: %d of %d tasks could not be described", len(out.Failures), len(arns))
	}
	obs, err := observations(out.Tasks, roleByARN)
	if err != nil {
		return nil, err
	}
	return platform.Aggregate(obs)
}

// listRunningTaskARNs pages through ecs:ListTasks for one service, filtered
// to the RUNNING desired status.
func (l *Lister) listRunningTaskARNs(ctx context.Context, service string) ([]string, error) {
	var arns []string
	paginator := awsecs.NewListTasksPaginator(l.client, &awsecs.ListTasksInput{
		Cluster:       &l.cfg.Cluster,
		ServiceName:   &service,
		DesiredStatus: types.DesiredStatusRunning,
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		arns = append(arns, page.TaskArns...)
	}
	return arns, nil
}

// observations maps DescribeTasks output to platform.Observation values. A
// RUNNING container with no valid digest fails the whole run rather than
// being dropped (docs/design/deployment-transparency/document.md,
// "Sanitizer").
func observations(tasks []types.Task, roleByARN map[string]role) ([]platform.Observation, error) {
	var obs []platform.Observation
	for _, t := range tasks {
		if t.LastStatus == nil || *t.LastStatus != "RUNNING" {
			continue
		}
		var arn string
		if t.TaskArn != nil {
			arn = *t.TaskArn
		}
		r, ok := roleByARN[arn]
		if !ok {
			continue
		}
		for _, c := range t.Containers {
			if c.LastStatus == nil || *c.LastStatus != "RUNNING" {
				continue
			}
			if c.Name == nil {
				continue
			}
			component, ok := mapComponent(r, *c.Name)
			if !ok {
				continue
			}
			if c.ImageDigest == nil || *c.ImageDigest == "" {
				return nil, fmt.Errorf("a running %s container in a %s task has no image digest", *c.Name, r)
			}
			if t.StartedAt == nil {
				return nil, fmt.Errorf("a running %s task has no start time", r)
			}
			obs = append(obs, platform.Observation{
				Component: component,
				Digest:    *c.ImageDigest,
				StartedAt: *t.StartedAt,
			})
		}
	}
	return obs, nil
}

// mapComponent is the pure form of the component mapping table
// (docs/design/deployment-transparency/document.md, "Component mapping"). It
// maps the service a task belongs to and a container's name to a document
// component, or reports that the container is not one this document
// describes.
func mapComponent(r role, container string) (string, bool) {
	switch r {
	case roleApp:
		switch container {
		case "server":
			return platform.Server, true
		case "caddy":
			return platform.Caddy, true
		}
	case roleWeb:
		if container == "web" {
			return platform.Web, true
		}
	case roleMaintenance:
		if container == "caddy" {
			return platform.Maintenance, true
		}
	}
	return "", false
}

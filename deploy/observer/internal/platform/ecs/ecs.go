package ecs

import (
	"context"
	"fmt"

	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"danny.vn/aboutme/deploy/observer/internal/platform"
)

// ecs:DescribeTasks accepts at most 100 tasks per call (Amazon ECS API reference).
const maxDescribeTasks = 100

type role string

const (
	roleApp         role = "app"
	roleWeb         role = "web"
	roleMaintenance role = "maintenance"
)

type Config struct {
	Cluster            string
	AppService         string
	WebService         string
	MaintenanceService string
}

type Lister struct {
	client *awsecs.Client
	cfg    Config
}

func New(client *awsecs.Client, cfg Config) *Lister {
	return &Lister{client: client, cfg: cfg}
}

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
	if len(out.Failures) > 0 {
		return nil, fmt.Errorf("describe tasks: %d of %d tasks could not be described", len(out.Failures), len(arns))
	}
	obs, err := observations(out.Tasks, roleByARN)
	if err != nil {
		return nil, err
	}
	return platform.Aggregate(obs)
}

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

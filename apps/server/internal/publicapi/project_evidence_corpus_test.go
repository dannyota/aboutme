package publicapi

import (
	"time"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

const (
	corpusEvidenceRepository = "https://github.com/ada/may-phan-tich"
	corpusEvidencePull       = "https://github.com/ada/may-phan-tich/pull/7"
	corpusEvidenceRelease    = "https://github.com/ada/may-phan-tich/releases/tag/v1.0"
	corpusEvidencePlain      = "https://github.com/babbage/engine/pull/12"
	corpusEvidencePlainTitle = `Sửa <lỗi> & "trích dẫn"`
)

func corpusEvidenceProject() publicresume.PublicProject {
	checked := time.Date(2026, time.November, 2, 15, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
	item := func(kind, title, url, repository string, at time.Time, confirmed bool) publicresume.PublicEvidence {
		return publicresume.PublicEvidence{Kind: kind, Title: title, URL: url, RepositoryName: repository, ItemDate: at, CheckedAt: checked, Confirmed: confirmed}
	}
	return publicresume.PublicProject{Slug: "may-phan-tich", Document: project.Document{
		DefaultLanguage: "vi", Text: map[string]project.Text{"vi": {Name: "Máy phân tích", Description: "<p>Ghi chú về máy tính cơ học.</p>"}},
	}, Evidence: []publicresume.PublicEvidence{
		item(project.EvidenceRepository, "ada/may-phan-tich", corpusEvidenceRepository, "ada/may-phan-tich", time.Date(2019, time.March, 4, 5, 6, 7, 0, time.UTC), true),
		item(project.EvidencePullRequest, "Thêm ghi chú G", corpusEvidencePull, "ada/may-phan-tich", time.Date(2026, time.January, 1, 6, 30, 0, 0, time.FixedZone("ICT", 7*60*60)), true),
		item(project.EvidenceRelease, "v1.0", corpusEvidenceRelease, "ada/may-phan-tich", time.Date(2024, time.July, 1, 0, 0, 0, 0, time.UTC), true),
		item(project.EvidencePullRequest, corpusEvidencePlainTitle, corpusEvidencePlain, "babbage/engine", time.Date(2023, time.May, 9, 10, 0, 0, 0, time.UTC), false),
	}}
}

func corpusEvidenceOnlyProject() publicresume.PublicProject {
	at := time.Date(2025, time.December, 31, 23, 30, 0, 0, time.UTC)
	return publicresume.PublicProject{Slug: "difference-engine", Document: project.Document{
		DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: "Difference Engine study"}},
	}, Evidence: []publicresume.PublicEvidence{
		{Kind: project.EvidenceRelease, Title: "Engine 2", URL: "https://github.com/ada/difference-engine/releases/tag/engine-2", RepositoryName: "ada/difference-engine", ItemDate: at, CheckedAt: at, Confirmed: true},
		{Kind: project.EvidenceRepository, Title: "ada/difference-engine", URL: "https://github.com/ada/difference-engine", RepositoryName: "ada/difference-engine", ItemDate: at, CheckedAt: at},
	}}
}

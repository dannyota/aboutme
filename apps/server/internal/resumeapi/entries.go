package resumeapi

import "net/http"

func entryRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/entries/{sectionKey}", Operation: "upsertResumeEntry", Mutation: true, Stub: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpsertResumeEntry},
		{Method: http.MethodDelete, Pattern: apiResumePath + "/{id}/entries/{sectionKey}/{entryId}", Operation: "deleteResumeEntry", Mutation: true, Stub: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleDeleteResumeEntry},
	}
}

func (s *Service) handleUpsertResumeEntry(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}
func (s *Service) handleDeleteResumeEntry(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}

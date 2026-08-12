package resumeapi

import "net/http"

func sectionRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/sections/{sectionKey}", Operation: "updateResumeSection", Mutation: true, Stub: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumeSection},
	}
}

func (s *Service) handleUpdateResumeSection(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}

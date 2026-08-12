package resumeapi

import "net/http"

func structureRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/structure", Operation: "updateResumeStructure", Mutation: true, Stub: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumeStructure},
	}
}

func (s *Service) handleUpdateResumeStructure(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}

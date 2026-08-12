package resumeapi

import "net/http"

func customizationRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/customization", Operation: "updateResumeCustomization", Mutation: true, Stub: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumeCustomization},
	}
}

func (s *Service) handleUpdateResumeCustomization(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}

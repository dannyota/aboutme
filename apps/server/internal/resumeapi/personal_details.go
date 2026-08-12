package resumeapi

import "net/http"

func personalDetailsRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/personal-details", Operation: "updateResumePersonalDetails", Mutation: true, Stub: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumePersonalDetails},
	}
}

func (s *Service) handleUpdateResumePersonalDetails(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}

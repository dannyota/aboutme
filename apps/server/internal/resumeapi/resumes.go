package resumeapi

import "net/http"

func resumeRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodGet, Pattern: apiResumePath, Operation: "listResumes", Stub: true, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleListResumes},
		{Method: http.MethodPost, Pattern: apiResumePath, Operation: "createResume", Mutation: true, Stub: true, OperationKind: operationCreate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleCreateResume},
		{Method: http.MethodGet, Pattern: apiResumePath + "/{id}", Operation: "getResume", Stub: true, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleGetResume},
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}", Operation: "updateResumeMetadata", Mutation: true, Stub: true, OperationKind: operationMetadata, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumeMetadata},
		{Method: http.MethodDelete, Pattern: apiResumePath + "/{id}", Operation: "deleteResume", Mutation: true, Stub: true, OperationKind: operationDelete, AcceptsWireVersion: true, Handler: (*Service).handleDeleteResume},
	}
}

func (s *Service) handleListResumes(w http.ResponseWriter, _ *http.Request) { writeConstructionStub(w) }
func (s *Service) handleCreateResume(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}
func (s *Service) handleGetResume(w http.ResponseWriter, _ *http.Request) { writeConstructionStub(w) }
func (s *Service) handleUpdateResumeMetadata(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}
func (s *Service) handleDeleteResume(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}

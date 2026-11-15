package mcpapi

const serverInstructions = "Use list_resumes or get_resume before changing an existing resume. " +
	"Pass the latest decimal revision to each existing-resume mutation and re-read after revision_conflict. " +
	"Every mutation requires a caller-generated idempotency_key UUID; reuse it only for an exact retry and choose a new key for changed intent. " +
	"Mutation results return the complete canonical stored resume. " +
	"Library projects use list_projects, get_project, create_project, update_project and delete_project; " +
	"link_project and refresh_project_entry connect a library project to a resume entry. " +
	"Publishing and public resume reads are not available."

package mcpapi

const serverInstructions = "Use list_resumes or get_resume before changing an existing resume. " +
	"Pass the latest decimal revision to each existing-resume mutation and re-read after revision_conflict. " +
	"Mutation results return the complete canonical stored resume. Publishing and public resume reads are not available."

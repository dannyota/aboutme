import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8")) as any;
const schemas = doc.components.schemas;

const SURFACE: Record<string, Record<string, string>> = {
  "/projects": { get: "listProjects", post: "createProject" },
  "/projects/{id}": {
    get: "getProject",
    put: "replaceProject",
    delete: "deleteProject",
  },
};

const STATUSES: Record<string, string[]> = {
  listProjects: ["200", "400", "401", "405", "429", "500"],
  createProject: [
    "201",
    "400",
    "401",
    "403",
    "405",
    "409",
    "413",
    "422",
    "429",
    "500",
  ],
  getProject: ["200", "400", "401", "404", "405", "429", "500"],
  replaceProject: [
    "200",
    "400",
    "401",
    "403",
    "404",
    "405",
    "409",
    "412",
    "413",
    "422",
    "428",
    "429",
    "500",
    "503",
  ],
  deleteProject: [
    "200",
    "400",
    "401",
    "403",
    "404",
    "405",
    "409",
    "412",
    "413",
    "428",
    "429",
    "500",
    "503",
  ],
};

const HTTP_METHODS = ["get", "put", "post", "patch", "delete", "head"];

const operations = () =>
  Object.entries(SURFACE).flatMap(([path, methods]) =>
    Object.entries(methods).map(
      ([method, id]) => [path, method, id, doc.paths[path][method]] as const,
    ),
  );

const refs = (operation: any) =>
  (operation.parameters ?? []).map((p: any) => p.$ref);

const resolve = (ref: string) =>
  ref
    .split("/")
    .slice(1)
    .reduce((node: any, key) => node[key], doc);

const responseCodes = (operation: any, status: string) => {
  const response = resolve(operation.responses[status].$ref);
  return [...String(response.description).matchAll(/`([a-z_]+)`/g)].map(
    (match) => match[1],
  );
};

describe("project library surface", () => {
  it("declares exactly the five owner library operations", () => {
    const declared = Object.keys(doc.paths).filter((p) =>
      p.startsWith("/projects"),
    );
    expect(declared.sort()).toEqual(Object.keys(SURFACE).sort());
    for (const [path, methods] of Object.entries(SURFACE)) {
      const item = doc.paths[path];
      const methodsDeclared = Object.keys(item).filter((k) =>
        HTTP_METHODS.includes(k),
      );
      expect(methodsDeclared.sort(), path).toEqual(Object.keys(methods).sort());
    }
    for (const [path, method, id, operation] of operations()) {
      expect(operation.operationId, `${method} ${path}`).toBe(id);
      expect(operation.tags, id).toEqual(["projects"]);
    }
    expect(doc.paths["/projects/{id}"].parameters).toEqual([
      { $ref: "#/components/parameters/ProjectID" },
    ]);
  });

  it("adds exactly the link and refresh resume routes", () => {
    const linkPaths = Object.keys(doc.paths).filter(
      (path) => path.startsWith("/resumes") && /project/.test(path),
    );
    expect(linkPaths.sort()).toEqual([
      "/resumes/{id}/entries/{entryId}/project-refresh",
      "/resumes/{id}/project-links",
    ]);
    for (const path of linkPaths) {
      const methods = Object.keys(doc.paths[path]).filter((k) =>
        HTTP_METHODS.includes(k),
      );
      expect(methods, path).toEqual(["post"]);
      expect(doc.paths[path].post.security, path).toEqual([
        { sessionCookie: [], csrfToken: [] },
      ]);
    }
    expect(schemas.ProjectLinkRequest.required).toEqual(["projectId"]);
    expect(schemas.ProjectLinkRequest.additionalProperties).toBe(false);
  });

  it("uses the cookie session only, with CSRF on every write", () => {
    for (const [, method, id, operation] of operations()) {
      expect(operation.security, id).toEqual(
        method === "get"
          ? [{ sessionCookie: [] }]
          : [{ sessionCookie: [], csrfToken: [] }],
      );
    }
  });

  it("requires an idempotency key on every write and If-Match except on create", () => {
    for (const [, method, id, operation] of operations()) {
      if (method === "get") {
        expect(operation.parameters, id).toBeUndefined();
        expect(operation.requestBody, id).toBeUndefined();
        continue;
      }
      expect(refs(operation), id).toContain(
        "#/components/parameters/IdempotencyKey",
      );
      if (id === "createProject") {
        expect(refs(operation)).not.toContain(
          "#/components/parameters/IfMatch",
        );
      } else {
        expect(refs(operation), id).toContain(
          "#/components/parameters/IfMatch",
        );
      }
    }
    expect(doc.paths["/projects/{id}"].delete.requestBody).toBeUndefined();
  });

  it("documents a closed status set per operation", () => {
    for (const [, , id, operation] of operations()) {
      expect(Object.keys(operation.responses).sort(), id).toEqual(
        [...STATUSES[id]].sort(),
      );
    }
  });

  it("names the project error codes", () => {
    const put = doc.paths["/projects/{id}"].put;
    expect(responseCodes(put, "409").sort()).toEqual([
      "idempotency_key_reuse",
      "project_limit",
      "project_slug_held",
      "project_slug_taken",
    ]);
    expect(responseCodes(put, "404")).toEqual(["project_not_found"]);
    expect(responseCodes(put, "403").sort()).toEqual([
      "csrf_rejected",
      "reauth_required",
    ]);
    expect(responseCodes(put, "422")).toEqual(["document_invalid"]);
    expect(responseCodes(put, "412")).toEqual(["revision_mismatch"]);
    expect(responseCodes(put, "503")).toEqual(["public_state_busy"]);
    const remove = doc.paths["/projects/{id}"].delete;
    expect(responseCodes(remove, "503")).toEqual(["public_state_busy"]);
    const mismatch = schemas.ProjectRevisionError.properties.error;
    expect(mismatch.required).toEqual(["code", "message", "details"]);
    expect(mismatch.properties.details.required).toEqual([
      "revision",
      "project",
    ]);
  });

  it("returns the project revision as a strong entity tag", () => {
    const etag = doc.components.headers.ProjectETag;
    expect(etag.schema.pattern).toBe('^"r[0-9]+"$');
    const create = doc.paths["/projects"].post.responses["201"];
    expect(create.headers.ETag.$ref).toBe("#/components/headers/ProjectETag");
    expect(create.headers.Location).toBeDefined();
    for (const method of ["get", "put"]) {
      expect(
        doc.paths["/projects/{id}"][method].responses["200"].headers.ETag.$ref,
        method,
      ).toBe("#/components/headers/ProjectETag");
    }
  });
});

describe("project library schemas", () => {
  it("pins the approved library limits", () => {
    const content = schemas.ProjectContent;
    expect(content.required).toEqual(["defaultLanguage", "text"]);
    expect(content.properties.technologies.maxItems).toBe(20);
    expect(content.properties.technologies.items.maxLength).toBe(40);
    expect(content.properties.text.minProperties).toBe(1);
    expect(content.properties.text.maxProperties).toBe(4);
    expect(content.properties.text.propertyNames.pattern).toBe("^[a-z]{2,8}$");
    const text = schemas.ProjectText;
    expect(text.additionalProperties).toBe(false);
    expect(
      Object.fromEntries(
        Object.entries<any>(text.properties).map(([k, v]) => [k, v.maxLength]),
      ),
    ).toEqual({
      name: 120,
      role: 120,
      summary: 300,
      outcome: 600,
      description: 16384,
    });
    expect(schemas.ProjectSlug).toMatchObject({
      minLength: 2,
      maxLength: 40,
      pattern: "^[a-z0-9]+(-[a-z0-9]+)*$",
    });
    expect(
      schemas.ProjectListResponse.properties.data.properties.projects.maxItems,
    ).toBe(30);
  });

  it("accepts only https links and closes every request and resource", () => {
    expect(schemas.ProjectLink.anyOf[1].pattern).toBe("^https://");
    expect(schemas.ProjectLink.maxLength).toBe(2048);
    for (const name of ["CreateProjectRequest", "Project"]) {
      expect(schemas[name].unevaluatedProperties, name).toBe(false);
    }
    expect(schemas.ReplaceProjectRequest.required).toEqual(["slug"]);
    for (const name of [
      "ProjectResponse",
      "ProjectListResponse",
      "ProjectReplaceResponse",
      "ProjectDeleteResponse",
      "ProjectRevisionError",
    ]) {
      expect(schemas[name].additionalProperties, name).toBe(false);
    }
  });

  it("bounds resume references by the three-resume account cap", () => {
    expect(schemas.Project.allOf[1].properties.linkedResumeIds.maxItems).toBe(
      3,
    );
    expect(schemas.ProjectChangedResumes.maxItems).toBe(3);
    expect(schemas.ProjectRecord.allOf[1].properties).not.toHaveProperty(
      "linkedResumeIds",
    );
  });
});

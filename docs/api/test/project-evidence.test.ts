import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8")) as any;
const schemas = doc.components.schemas;

const SURFACE: Record<string, Record<string, string>> = {
  "/projects/{id}/evidence/candidates": {
    get: "listProjectEvidenceCandidates",
  },
  "/projects/{id}/evidence": { post: "attachProjectEvidence" },
  "/projects/{id}/evidence/order": { put: "reorderProjectEvidence" },
  "/projects/{id}/evidence/{evidenceId}": { delete: "removeProjectEvidence" },
  "/projects/{id}/evidence/{evidenceId}/recheck": {
    post: "recheckProjectEvidence",
  },
};

const WRITE = [
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
];

const STATUSES: Record<string, string[]> = {
  listProjectEvidenceCandidates: [
    "200",
    "400",
    "401",
    "403",
    "404",
    "405",
    "409",
    "429",
    "500",
    "502",
  ],
  attachProjectEvidence: [...WRITE, "422", "502"],
  reorderProjectEvidence: WRITE,
  removeProjectEvidence: WRITE,
  recheckProjectEvidence: [...WRITE, "502"],
};

const HTTP_METHODS = ["get", "put", "post", "patch", "delete", "head"];

const operations = () =>
  Object.entries(SURFACE).flatMap(([path, methods]) =>
    Object.entries(methods).map(
      ([method, id]) => [path, method, id, doc.paths[path][method]] as const,
    ),
  );

const operation = (id: string) =>
  operations().find(([, , operationId]) => operationId === id)![3];

const resolve = (ref: string) =>
  ref
    .split("/")
    .slice(1)
    .reduce((node: any, key) => node[key], doc);

const codes = (op: any, status: string) => {
  const response = op.responses[status].$ref
    ? resolve(op.responses[status].$ref)
    : op.responses[status];
  return [...String(response.description).matchAll(/`([a-z]+_[a-z_]+)`/g)]
    .map((match) => match[1])
    .sort();
};

const refs = (op: any) => (op.parameters ?? []).map((p: any) => p.$ref);

describe("project evidence surface", () => {
  it("declares exactly the five evidence operations under a project", () => {
    const declared = Object.keys(doc.paths).filter((p) =>
      p.startsWith("/projects/{id}/evidence"),
    );
    expect(declared.sort()).toEqual(Object.keys(SURFACE).sort());
    for (const [path, methods] of Object.entries(SURFACE)) {
      const methodsDeclared = Object.keys(doc.paths[path]).filter((k) =>
        HTTP_METHODS.includes(k),
      );
      expect(methodsDeclared, path).toEqual(Object.keys(methods));
      expect(doc.paths[path].parameters[0], path).toEqual({
        $ref: "#/components/parameters/ProjectID",
      });
    }
    for (const [path, method, id, op] of operations()) {
      expect(op.operationId, `${method} ${path}`).toBe(id);
      expect(op.tags, id).toEqual(["githubEvidence"]);
    }
  });

  it("uses the cookie session only, with CSRF on every operation", () => {
    for (const [, , id, op] of operations()) {
      expect(op.security, id).toEqual([{ sessionCookie: [], csrfToken: [] }]);
    }
  });

  it("refuses a candidates read without a valid CSRF token", () => {
    const op = operation("listProjectEvidenceCandidates");
    expect(op.security).toEqual([{ sessionCookie: [], csrfToken: [] }]);
    expect(op.description).toContain("valid `X-CSRF-Token`");
    expect(op.description).not.toContain("Sec-Fetch-Site");
    expect(op.description).toContain("`403 csrf_rejected`");
    expect(codes(op, "403")).toEqual(["csrf_rejected"]);
  });

  it("requires If-Match and an idempotency key on every write", () => {
    for (const [, method, id, op] of operations()) {
      if (method === "get") {
        expect(refs(op), id).toEqual([undefined, undefined, undefined]);
        expect(op.requestBody, id).toBeUndefined();
        continue;
      }
      expect(refs(op), id).toEqual([
        "#/components/parameters/IfMatch",
        "#/components/parameters/IdempotencyKey",
      ]);
      expect(resolve(op.responses["200"].$ref).headers.ETag.$ref, id).toBe(
        "#/components/headers/ProjectETag",
      );
    }
    for (const id of ["removeProjectEvidence", "recheckProjectEvidence"]) {
      expect(operation(id).requestBody, id).toBeUndefined();
    }
  });

  it("documents a closed status set per operation", () => {
    for (const [, , id, op] of operations()) {
      expect(Object.keys(op.responses).sort(), id).toEqual(
        [...STATUSES[id]].sort(),
      );
    }
  });

  it("names the candidate query and its errors", () => {
    const list = operation("listProjectEvidenceCandidates");
    expect(list.parameters.map((p: any) => [p.name, p.in, p.required])).toEqual(
      [
        ["kind", "query", true],
        ["page", "query", true],
        ["repository", "query", false],
      ],
    );
    expect(list.parameters[1].schema).toMatchObject({
      minimum: 1,
      maximum: 10,
    });
    expect(codes(list, "400")).toEqual(["request_invalid"]);
    expect(codes(list, "404")).toEqual([
      "evidence_unavailable",
      "project_not_found",
    ]);
    expect(codes(list, "409")).toEqual(["evidence_session_required"]);
    expect(codes(list, "429")).toEqual(["rate_limited"]);
    expect(codes(list, "502")).toEqual(["evidence_upstream"]);
  });

  it("names the write error codes", () => {
    const attach = operation("attachProjectEvidence");
    expect(codes(attach, "409")).toEqual([
      "evidence_duplicate",
      "evidence_session_required",
      "idempotency_key_reuse",
    ]);
    expect(codes(attach, "422")).toEqual([
      "document_invalid",
      "evidence_limit",
      "evidence_not_confirmed",
      "evidence_unsupported_link",
    ]);
    expect(codes(attach, "404")).toEqual([
      "evidence_unavailable",
      "project_not_found",
    ]);
    for (const id of [
      "reorderProjectEvidence",
      "removeProjectEvidence",
      "recheckProjectEvidence",
    ]) {
      expect(codes(operation(id), "404"), id).toEqual([
        "evidence_not_found",
        "evidence_unavailable",
        "project_not_found",
      ]);
    }
    expect(codes(operation("recheckProjectEvidence"), "409")).toEqual([
      "evidence_session_required",
      "idempotency_key_reuse",
    ]);
    for (const [, method, id, op] of operations()) {
      if (method === "get") continue;
      expect(codes(op, "400"), id).toEqual([
        "idempotency_key_invalid",
        "idempotency_key_required",
        "precondition_malformed",
        "request_invalid",
      ]);
      expect(codes(op, "412"), id).toEqual(["revision_mismatch"]);
      expect(codes(op, "413"), id).toEqual(["body_too_large"]);
      expect(codes(op, "503"), id).toEqual(["public_state_busy"]);
      expect(codes(op, "429"), id).toEqual(["rate_limited"]);
      expect(
        resolve(op.responses["429"].$ref).headers["Retry-After"],
        id,
      ).toBeDefined();
    }
  });
});

describe("project evidence schemas", () => {
  it("closes the read model and pins its bounds", () => {
    const item = schemas.ProjectEvidence;
    expect(item.additionalProperties).toBe(false);
    expect(item.required).toEqual([
      "id",
      "kind",
      "url",
      "repositoryName",
      "title",
      "itemDate",
      "checkedAt",
      "confirmed",
      "flagReason",
    ]);
    expect(Object.keys(item.properties)).toEqual(item.required);
    expect(item.properties.url).toMatchObject({
      maxLength: 300,
      pattern: "^https://github\\.com/",
    });
    expect(item.properties.title.maxLength).toBe(200);
    expect(item.properties.repositoryName.maxLength).toBe(200);
    expect(item.properties.flagReason.enum).toEqual([
      "rule_failed",
      "gone",
      null,
    ]);
    expect(schemas.ProjectEvidenceKind.enum).toEqual([
      "repository",
      "pullRequest",
      "release",
    ]);
    expect(schemas.ProjectEvidenceList.maxItems).toBe(10);
  });

  it("carries evidence on a single project but never requires it", () => {
    const project = schemas.Project.allOf[1];
    expect(project.properties.evidence.$ref).toBe(
      "#/components/schemas/ProjectEvidenceList",
    );
    expect(project.required).toEqual(["linkedResumeIds"]);
    expect(schemas.ProjectRecord.allOf[1].properties).not.toHaveProperty(
      "evidence",
    );
    expect(schemas.CreateProjectRequest.unevaluatedProperties).toBe(false);
    expect(
      schemas.CreateProjectRequest.allOf.flatMap((part: any) =>
        Object.keys(part.properties ?? {}),
      ),
    ).not.toContain("evidence");
  });

  it("closes the candidate page and the write bodies", () => {
    const page = schemas.ProjectEvidenceCandidatesResponse.properties.data;
    expect(page.required).toEqual(["items", "nextPage", "incomplete"]);
    expect(page.properties.items.maxItems).toBe(30);
    expect(page.properties.nextPage.type).toEqual(["integer", "null"]);
    const candidate = schemas.ProjectEvidenceCandidate;
    expect(candidate.required).toEqual([
      "candidateId",
      "kind",
      "title",
      "repository",
      "date",
      "attached",
    ]);
    expect(candidate.properties.repository.properties.id.type).toEqual([
      "integer",
      "null",
    ]);
    const attach = schemas.ProjectEvidenceAttachRequest;
    expect(attach.additionalProperties).toBe(false);
    expect(attach.required).toEqual(["kind"]);
    expect(attach.oneOf).toEqual([
      { required: ["link"] },
      { required: ["candidateId"] },
    ]);
    const order = schemas.ProjectEvidenceOrderRequest;
    expect(order.additionalProperties).toBe(false);
    expect(order.properties.order).toMatchObject({
      maxItems: 10,
      uniqueItems: true,
    });
    const written = schemas.ProjectEvidenceWriteResponse.properties.data;
    expect(written.required).toEqual([
      "evidence",
      "projectRevision",
      "resumeRevisions",
    ]);
    expect(written.properties.projectRevision.$ref).toBe(
      "#/components/schemas/Revision",
    );
    expect(written.properties.resumeRevisions.$ref).toBe(
      "#/components/schemas/ProjectChangedResumes",
    );
  });

  it("names the evidence error codes", () => {
    expect(schemas.GitHubEvidenceErrorCode.enum).toEqual([
      "evidence_unavailable",
      "evidence_session_required",
      "evidence_not_confirmed",
      "evidence_duplicate",
      "evidence_limit",
      "evidence_unsupported_link",
      "evidence_upstream",
      "evidence_connection_changed",
      "evidence_not_found",
    ]);
  });
});

describe("GitHub evidence connection", () => {
  const item = doc.paths["/github-evidence"];

  it("reports the connection generation", () => {
    const status = schemas.GitHubEvidenceStatus;
    expect(status.required).toEqual([
      "generation",
      "hasEvidence",
      "connection",
      "session",
    ]);
    expect(status.properties.generation).toMatchObject({
      type: "integer",
      minimum: 0,
    });
    expect(status.properties.hasEvidence).toMatchObject({ type: "boolean" });
  });

  it("guards disconnect with the generation and an idempotency key", () => {
    const disconnect = item.delete;
    expect(disconnect.security).toEqual([{ sessionCookie: [], csrfToken: [] }]);
    expect(disconnect.requestBody).toBeUndefined();
    const params = disconnect.parameters;
    expect(
      params.slice(0, 2).map((p: any) => [p.name, p.in, p.required]),
    ).toEqual([
      ["keepLinks", "query", true],
      ["generation", "query", true],
    ]);
    expect(params[0].schema.enum).toEqual(["true", "false"]);
    expect(params[1].schema).toMatchObject({ type: "integer", minimum: 0 });
    expect(params[2]).toEqual({
      $ref: "#/components/parameters/IdempotencyKey",
    });
    expect(refs(disconnect)).not.toContain("#/components/parameters/IfMatch");
    expect(Object.keys(disconnect.responses).sort()).toEqual(
      ["200", "400", "401", "403", "404", "405", "409", "429", "503"].sort(),
    );
    expect(disconnect.responses["200"].headers).toBeUndefined();
    expect(
      disconnect.responses["200"].content["application/json"].schema.$ref,
    ).toBe("#/components/schemas/GitHubEvidenceDisconnectResponse");
    expect(codes(disconnect, "400")).toEqual([
      "idempotency_key_invalid",
      "idempotency_key_required",
      "request_invalid",
    ]);
    expect(codes(disconnect, "409")).toEqual([
      "evidence_connection_changed",
      "idempotency_key_reuse",
    ]);
    expect(codes(disconnect, "503")).toEqual(["public_state_busy"]);
    const body = schemas.GitHubEvidenceDisconnectResponse.properties.data;
    expect(body.required).toEqual([
      "generation",
      "projectRevisions",
      "resumeRevisions",
    ]);
    expect(body.properties.projectRevisions.$ref).toBe(
      "#/components/schemas/ProjectRevisions",
    );
    expect(body.properties.resumeRevisions.$ref).toBe(
      "#/components/schemas/ProjectChangedResumes",
    );
    expect(schemas.ProjectRevisions.items.required).toEqual(["id", "revision"]);
  });

  it("takes an optional start body with only returnTo", () => {
    const start = doc.paths["/github-evidence/start"].post;
    expect(start.requestBody.required).toBe(false);
    const body = schemas.GitHubEvidenceStartRequest;
    expect(body.additionalProperties).toBe(false);
    expect(body.required).toBeUndefined();
    expect(Object.keys(body.properties)).toEqual(["returnTo"]);
  });

  it("names every callback outcome", () => {
    const callback = doc.paths["/github-evidence/callback"].get;
    const redirect = String(callback.responses["302"].description);
    for (const value of ["connected", "failed", "different_account"]) {
      expect(redirect).toContain(`\`${value}\``);
    }
    expect(redirect).toContain("/app/projects/{id}");
    expect(redirect).toContain("/app/settings/sessions");
  });
});

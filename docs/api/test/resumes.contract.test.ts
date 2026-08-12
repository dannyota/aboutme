import { existsSync, readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8"));
const redoclyConfig = parse(readFileSync("docs/api/redocly.yaml", "utf8"));

// The P2B surface, exactly. Source: docs/plans/phase-2b/task-01-openapi-contract.md.
// operationIds are frozen: ADR 0016's idempotency operation identity hashes the
// exact OpenAPI operationId string, so renaming one silently changes every
// stored replay record's identity.
const SURFACE: Record<string, Record<string, string>> = {
  "/resumes": { get: "listResumes", post: "createResume" },
  "/resumes/{id}": {
    get: "getResume",
    patch: "updateResumeMetadata",
    delete: "deleteResume",
  },
  "/resumes/{id}/entries/{sectionKey}": { patch: "upsertResumeEntry" },
  "/resumes/{id}/entries/{sectionKey}/{entryId}": {
    delete: "deleteResumeEntry",
  },
  "/resumes/{id}/sections/{sectionKey}": { patch: "updateResumeSection" },
  "/resumes/{id}/structure": { patch: "updateResumeStructure" },
  "/resumes/{id}/personal-details": { patch: "updateResumePersonalDetails" },
  "/resumes/{id}/customization": { patch: "updateResumeCustomization" },
  "/resumes/{id}/photo": {
    post: "uploadResumePhoto",
    get: "getResumePhoto",
    patch: "updateResumePhotoCrop",
    delete: "deleteResumePhoto",
  },
};

const MUTATIONS = new Set(["post", "patch", "delete"]);

interface Success {
  status: string;
  mediaTypes: string[]; // empty means a bodyless response
  headers: string[];
}

// The exact success contract. A parent ETag is `"r<revision>"`; the photo GET's
// object ETag is derived from the immutable normalized object key.
const SUCCESS: Record<string, Success[]> = {
  listResumes: [
    {
      status: "200",
      mediaTypes: ["application/json"],
      headers: ["X-Resume-Schema-Version"],
    },
  ],
  createResume: [
    {
      status: "201",
      mediaTypes: ["application/json"],
      headers: ["Location", "ETag", "X-Resume-Schema-Version"],
    },
  ],
  getResume: [
    {
      status: "200",
      mediaTypes: ["application/json"],
      headers: ["ETag", "X-Resume-Schema-Version"],
    },
  ],
  updateResumeMetadata: [
    {
      status: "200",
      mediaTypes: ["application/json"],
      headers: ["ETag", "X-Resume-Schema-Version"],
    },
  ],
  deleteResume: [{ status: "204", mediaTypes: [], headers: [] }],
  upsertResumeEntry: [
    {
      status: "200",
      mediaTypes: ["application/json"],
      headers: ["ETag", "X-Resume-Schema-Version"],
    },
  ],
  deleteResumeEntry: [
    {
      status: "204",
      mediaTypes: [],
      headers: ["ETag", "X-Resume-Schema-Version"],
    },
  ],
  updateResumeSection: [
    {
      status: "200",
      mediaTypes: ["application/json"],
      headers: ["ETag", "X-Resume-Schema-Version"],
    },
  ],
  updateResumeStructure: [
    {
      status: "200",
      mediaTypes: ["application/json"],
      headers: ["ETag", "X-Resume-Schema-Version"],
    },
  ],
  updateResumePersonalDetails: [
    {
      status: "200",
      mediaTypes: ["application/json"],
      headers: ["ETag", "X-Resume-Schema-Version"],
    },
  ],
  updateResumeCustomization: [
    {
      status: "200",
      mediaTypes: ["application/json"],
      headers: ["ETag", "X-Resume-Schema-Version"],
    },
  ],
  uploadResumePhoto: [
    {
      status: "200",
      mediaTypes: ["application/json"],
      headers: ["ETag", "X-Resume-Schema-Version"],
    },
  ],
  updateResumePhotoCrop: [
    {
      status: "200",
      mediaTypes: ["application/json"],
      headers: ["ETag", "X-Resume-Schema-Version"],
    },
  ],
  getResumePhoto: [
    {
      status: "200",
      mediaTypes: ["image/jpeg", "image/png"],
      headers: ["ETag"],
    },
    { status: "304", mediaTypes: [], headers: ["ETag"] },
  ],
  deleteResumePhoto: [
    {
      status: "204",
      mediaTypes: [],
      headers: ["ETag", "X-Resume-Schema-Version"],
    },
  ],
};

const COMMON_400 = ["invalid_client_ip"];
const WRITE_400 = [
  ...COMMON_400,
  "idempotency_key_required",
  "idempotency_key_invalid",
  "precondition_malformed",
  "request_invalid",
  "unsupported_schema_version",
];
const CREATE_400 = [
  ...COMMON_400,
  "idempotency_key_required",
  "idempotency_key_invalid",
  "precondition_not_supported",
  "request_invalid",
  "unsupported_schema_version",
];

// Only reachable responses. A safe read never documents CSRF or precondition
// failure; a JSON route never documents a media failure.
const itemWrite = (extra: Record<string, string[]> = {}) => ({
  "400": WRITE_400,
  "401": ["session_required"],
  "403": ["csrf_rejected"],
  "404": ["resume_not_found"],
  "405": ["method_not_allowed"],
  "409": ["idempotency_key_reuse"],
  "412": ["revision_mismatch"],
  "413": ["body_too_large"],
  "422": ["document_invalid"],
  "428": ["precondition_required"],
  "429": ["rate_limited"],
  "500": ["internal_error"],
  ...extra,
});

const itemDelete = (extra: Record<string, string[]> = {}) => {
  const { "422": _dropped, ...rest } = itemWrite();
  return { ...rest, ...extra };
};

const ERRORS: Record<string, Record<string, string[]>> = {
  listResumes: {
    "400": [...COMMON_400, "unsupported_schema_version"],
    "401": ["session_required"],
    "405": ["method_not_allowed"],
    "429": ["rate_limited"],
    "500": ["internal_error"],
  },
  createResume: {
    "400": CREATE_400,
    "401": ["session_required"],
    "403": ["csrf_rejected"],
    "405": ["method_not_allowed"],
    "409": ["resume_cap_exceeded", "idempotency_key_reuse"],
    "413": ["body_too_large"],
    "422": ["document_invalid"],
    "429": ["rate_limited"],
    "500": ["internal_error"],
  },
  getResume: {
    "400": [...COMMON_400, "request_invalid", "unsupported_schema_version"],
    "401": ["session_required"],
    "404": ["resume_not_found"],
    "405": ["method_not_allowed"],
    "429": ["rate_limited"],
    "500": ["internal_error"],
  },
  updateResumeMetadata: itemWrite(),
  deleteResume: itemDelete(),
  upsertResumeEntry: itemWrite(),
  deleteResumeEntry: itemDelete(),
  updateResumeSection: itemWrite(),
  updateResumeStructure: itemWrite(),
  updateResumePersonalDetails: itemWrite(),
  updateResumeCustomization: itemWrite({
    "422": ["document_invalid", "customization_path_denied"],
  }),
  uploadResumePhoto: {
    "400": WRITE_400,
    "401": ["session_required"],
    "403": ["csrf_rejected"],
    "404": ["resume_not_found"],
    "405": ["method_not_allowed"],
    "409": ["idempotency_key_reuse"],
    "412": ["revision_mismatch"],
    "413": ["media_too_large"],
    "415": ["media_type_unsupported"],
    "422": ["media_invalid", "document_invalid"],
    "428": ["precondition_required"],
    "429": ["rate_limited"],
    "500": ["internal_error"],
    "503": ["media_busy"],
  },
  updateResumePhotoCrop: itemWrite({
    "404": ["resume_not_found", "media_not_found"],
  }),
  getResumePhoto: {
    "400": [...COMMON_400, "request_invalid"],
    "401": ["session_required"],
    "404": ["resume_not_found", "media_not_found"],
    "405": ["method_not_allowed"],
    "429": ["rate_limited"],
    "500": ["internal_error"],
  },
  deleteResumePhoto: itemDelete({
    "404": ["resume_not_found", "media_not_found"],
  }),
};

const operations = (): [string, string, string, any][] =>
  Object.entries(SURFACE).flatMap(([path, methods]) =>
    Object.entries(methods).map(
      ([method, id]) =>
        [path, method, id, doc.paths?.[path]?.[method]] as [
          string,
          string,
          string,
          any,
        ],
    ),
  );

// Shared error responses are declared once in components.responses and
// referenced per operation, so every assertion below reads through the ref.
const resolve = (node: any): any => {
  if (!node?.$ref) return node;
  const name = String(node.$ref).replace("#/components/responses/", "");
  const target = doc.components.responses[name];
  expect(target, `unresolved response ref ${node.$ref}`).toBeTruthy();
  return target;
};

const paramRefs = (operation: any): string[] =>
  (operation?.parameters ?? []).map((p: any) => p.$ref ?? `${p.in}:${p.name}`);

const codesIn = (response: any): string[] => {
  const found = new Set<string>();
  const walk = (node: any) => {
    if (!node || typeof node !== "object") return;
    if (Array.isArray(node)) {
      node.forEach(walk);
      return;
    }
    for (const [key, value] of Object.entries<any>(node)) {
      if (key === "code" && typeof value === "string") found.add(value);
      else walk(value);
    }
  };
  walk(response?.content);
  // Codes that only ever appear in prose are still part of the contract, so
  // the description is scanned for backtick-quoted code literals too.
  for (const match of String(response?.description ?? "").matchAll(
    /`([a-z_]+)`/g,
  )) {
    found.add(match[1]);
  }
  return [...found];
};

describe("P2B resume surface", () => {
  it("declares exactly the nine resume paths and fifteen operations", () => {
    const declared = Object.keys(doc.paths).filter((p) =>
      p.startsWith("/resumes"),
    );
    expect(declared.sort()).toEqual(Object.keys(SURFACE).sort());

    for (const [path, methods] of Object.entries(SURFACE)) {
      const item = doc.paths[path];
      const httpMethods = Object.keys(item).filter((k) =>
        ["get", "put", "post", "patch", "delete", "head", "options"].includes(
          k,
        ),
      );
      expect(httpMethods.sort(), `${path} methods`).toEqual(
        Object.keys(methods).sort(),
      );
      for (const [method, id] of Object.entries(methods)) {
        expect(item[method].operationId, `${method} ${path}`).toBe(id);
      }
    }

    const ids = Object.values<any>(doc.paths).flatMap((item) =>
      Object.values<any>(item)
        .filter((op) => op && typeof op === "object" && op.operationId)
        .map((op) => op.operationId),
    );
    expect(new Set(ids).size, "operationIds are unique").toBe(ids.length);
  });

  it("requires an idempotency key on every mutation", () => {
    for (const [path, method, id, operation] of operations()) {
      if (!MUTATIONS.has(method)) continue;
      expect(
        paramRefs(operation),
        `${id} (${method} ${path}) must reuse the shared Idempotency-Key`,
      ).toContain("#/components/parameters/IdempotencyKey");
    }
  });

  it("requires If-Match on every mutation except resume creation (D6)", () => {
    for (const [path, method, id, operation] of operations()) {
      if (!MUTATIONS.has(method)) continue;
      const refs = paramRefs(operation);
      const wanted = "#/components/parameters/IfMatch";
      if (id === "createResume") {
        expect(refs, "create has no prior revision").not.toContain(wanted);
      } else {
        expect(refs, `${id} (${method} ${path})`).toContain(wanted);
      }
    }
  });

  it("declares sessionCookie everywhere and csrfToken on every mutation", () => {
    for (const [path, method, id, operation] of operations()) {
      const expected = MUTATIONS.has(method)
        ? [{ sessionCookie: [], csrfToken: [] }]
        : [{ sessionCookie: [] }];
      expect(operation.security, `${id} (${method} ${path})`).toEqual(expected);
    }
  });

  it("matches the success contract exactly, including three distinct 204s", () => {
    for (const [path, method, id, operation] of operations()) {
      const expected = SUCCESS[id];
      const declared = Object.keys(operation.responses).filter((s) =>
        /^[23]/.test(s),
      );
      expect(declared.sort(), `${id} success statuses`).toEqual(
        expected.map((s) => s.status).sort(),
      );

      for (const success of expected) {
        const response = resolve(operation.responses[success.status]);
        const label = `${id} ${success.status} (${method} ${path})`;
        expect(
          Object.keys(response.content ?? {}).sort(),
          `${label} content`,
        ).toEqual(success.mediaTypes.slice().sort());
        expect(
          Object.keys(response.headers ?? {}).sort(),
          `${label} headers`,
        ).toEqual(success.headers.slice().sort());
      }
    }
  });

  it("matches the error contract exactly, with no impossible response", () => {
    for (const [path, method, id, operation] of operations()) {
      const expected = ERRORS[id];
      const declared = Object.keys(operation.responses).filter((s) =>
        /^[45]/.test(s),
      );
      expect(
        declared.sort(),
        `${id} error statuses (${method} ${path})`,
      ).toEqual(Object.keys(expected).sort());

      for (const [status, codes] of Object.entries(expected)) {
        const response = resolve(operation.responses[status]);
        const label = `${id} ${status}`;
        expect(
          response.content?.["application/json"]?.schema?.$ref ?? "",
          `${label} uses the Error envelope`,
        ).toContain("Error");
        const documented = codesIn(response);
        for (const code of codes) {
          expect(documented, `${label} documents ${code}`).toContain(code);
        }
      }
    }
  });

  it("carries Allow on every 405 and never documents 501", () => {
    for (const [path, method, id, operation] of operations()) {
      const methodNotAllowed = resolve(operation.responses["405"]);
      expect(
        methodNotAllowed.headers?.Allow,
        `${id} 405 declares Allow (${method} ${path})`,
      ).toBeTruthy();
      const allowed = Object.keys(SURFACE[path])
        .map((m) => m.toUpperCase())
        .sort()
        .join(", ");
      expect(methodNotAllowed.headers.Allow.example, `${id} Allow value`).toBe(
        allowed,
      );
      expect(operation.responses["501"], `${id} has no 501`).toBeUndefined();
    }
    expect(JSON.stringify(doc)).not.toContain("not_implemented");
  });

  it("amends Error with an optional details object (D7)", () => {
    const error = doc.components.schemas.Error;
    expect(error.properties.error.required).toEqual(["code", "message"]);
    const details = error.properties.error.properties.details;
    expect(details, "details exists").toBeTruthy();
    expect(details.type).toBe("object");
    expect(error.properties.error.required).not.toContain("details");
    for (const key of ["revision", "document", "issues", "acceptedVersions"]) {
      expect(JSON.stringify(details), `details documents ${key}`).toContain(
        key,
      );
    }
  });

  it("declares the resume parameters and singleton header handling", () => {
    const params = doc.components.parameters;
    for (const name of ["ResumeID", "SectionKey", "EntryID"]) {
      expect(params[name]?.in, `${name} is a path parameter`).toBe("path");
      expect(params[name].required).toBe(true);
    }

    const version = params.SchemaVersionHeader;
    expect(version.name).toBe("X-Resume-Schema-Version");
    expect(version.in).toBe("header");
    expect(version.required).toBe(false);

    const ifNoneMatch = params.IfNoneMatch;
    expect(ifNoneMatch.name).toBe("If-None-Match");
    expect(ifNoneMatch.required).toBe(false);

    for (const name of [
      "IdempotencyKey",
      "IfMatch",
      "SchemaVersionHeader",
      "IfNoneMatch",
    ]) {
      expect(
        params[name].description,
        `${name} rejects repeated field lines and comma folding`,
      ).toMatch(/repeated|singleton/i);
      expect(params[name].description).toMatch(/comma|folded/i);
    }
  });

  it("puts the schema-version header on every JSON resume operation, never on the binary read", () => {
    for (const [path, method, id, operation] of operations()) {
      const refs = paramRefs(operation);
      const wanted = "#/components/parameters/SchemaVersionHeader";
      if (id === "getResumePhoto") {
        expect(
          refs,
          "binary photo GET is outside the version contract",
        ).not.toContain(wanted);
        expect(refs).toContain("#/components/parameters/IfNoneMatch");
      } else {
        expect(refs, `${id} (${method} ${path})`).toContain(wanted);
      }
    }
  });

  it("keeps deletes bodyless with a zero-byte idempotency payload", () => {
    for (const [path, method, id, operation] of operations()) {
      if (method !== "delete") continue;
      expect(
        operation.requestBody,
        `${id} declares no request body (${path})`,
      ).toBeUndefined();
      expect(
        operation.description,
        `${id} pins its idempotency payload`,
      ).toMatch(/zero-length|zero bytes/i);
    }
  });

  it("keeps the document opaque and points at its owning schema (D16)", () => {
    for (const name of [
      "ResumeDocument",
      "PersonalDetailsPatch",
      "CustomizationDelta",
      "StructureCommand",
      "EntryUpsert",
      "SectionPatch",
      "PhotoCropPatch",
      "ResumeSummary",
      "Resume",
    ]) {
      expect(doc.components.schemas[name], `${name} exists`).toBeTruthy();
    }
    const description = doc.components.schemas.ResumeDocument.description;
    expect(description).toContain("packages/schema/resume.schema.json");
    expect(description).toContain("packages/schema/gen/ts");
    expect(description).toContain("X-Resume-Schema-Version");
    expect(existsSync("packages/schema/resume.schema.json")).toBe(true);
    expect(existsSync("packages/schema/gen/ts")).toBe(true);
  });

  it("guards the server-owned photo field out of the personal-details patch", () => {
    const patch = doc.components.schemas.PersonalDetailsPatch;
    expect(patch.not).toEqual({ required: ["photo"] });

    const crop = doc.components.schemas.PhotoCropPatch;
    expect(Object.keys(crop.properties)).toEqual(["crop"]);
    expect(crop.additionalProperties).toBe(false);
    expect(JSON.stringify(crop)).toContain("$defs/photoCrop");
    expect(crop.properties.crop.description).toMatch(/null/i);
  });

  it("bounds the two ordered command lists at 100 items", () => {
    // The components are single commands; the 100-item bound belongs to the
    // request bodies that carry the ordered list.
    for (const name of ["StructureCommand", "CustomizationDelta"]) {
      expect(doc.components.schemas[name].type, `${name} is one command`).toBe(
        "object",
      );
    }
    const structure =
      doc.paths["/resumes/{id}/structure"].patch.requestBody.content[
        "application/json"
      ].schema;
    const customization =
      doc.paths["/resumes/{id}/customization"].patch.requestBody.content[
        "application/json"
      ].schema;
    for (const [label, body] of [
      ["structure", structure],
      ["customization", customization],
    ] as const) {
      const commands = body.properties.commands ?? body.properties.deltas;
      expect(commands.type, `${label} body is a list`).toBe("array");
      expect(commands.maxItems, `${label} maxItems`).toBe(100);
    }
  });

  it("pins zero-based structure indexes evaluated in command order", () => {
    const schema = doc.components.schemas.StructureCommand;
    const text = JSON.stringify(schema);
    expect(text).toContain('"minimum":0');
    expect(schema.description).toMatch(/zero-based/i);
    expect(schema.description).toMatch(/append/i);
    expect(schema.description).toMatch(
      /remove[sd]? the source key first|removed first/i,
    );
    expect(schema.description).toMatch(/in list order|sequential/i);
  });

  it("enforces example validity through the linter, not by inspection", () => {
    expect(redoclyConfig.rules["no-invalid-media-type-examples"]).toBe("error");
    expect(redoclyConfig.rules["no-invalid-schema-examples"]).toBe("error");
  });
});

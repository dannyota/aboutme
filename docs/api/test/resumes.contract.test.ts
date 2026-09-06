import { existsSync, readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8"));
const redoclyConfig = parse(readFileSync("docs/api/redocly.yaml", "utf8"));

// The private resume surface, exactly (docs/design/api.md, "Endpoint groups").
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
  "/resumes/{id}/publish": { post: "publishResume" },
  "/resumes/{id}/pdf": { get: "downloadResumePDF", head: "headResumePDF" },
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
  downloadResumePDF: [{
    status: "200", mediaTypes: ["application/pdf"],
    headers: ["Cache-Control", "Content-Disposition", "Content-Length"],
  }],
  headResumePDF: [{
    status: "200", mediaTypes: ["application/pdf"],
    headers: ["Cache-Control", "Content-Disposition", "Content-Length"],
  }],
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
  publishResume: [
    {
      status: "200",
      mediaTypes: ["application/json"],
      headers: ["ETag", "X-Resume-Schema-Version"],
    },
  ],
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
  downloadResumePDF: {
    "400": [...COMMON_400, "request_invalid"],
    "401": ["session_required"], "404": ["resume_not_found"],
    "405": ["method_not_allowed"], "429": ["rate_limited"],
    "503": ["internal_error"],
  },
  headResumePDF: {
    "400": [...COMMON_400, "request_invalid"],
    "401": ["session_required"], "404": ["resume_not_found"],
    "405": ["method_not_allowed"], "429": ["rate_limited"],
    "503": ["internal_error"],
  },
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
  publishResume: {
    "400": WRITE_400,
    "401": ["session_required"],
    "403": ["csrf_rejected", "reauth_required"],
    "404": ["resume_not_found"],
    "405": ["method_not_allowed"],
    "409": ["slug_taken", "idempotency_key_reuse"],
    "412": ["revision_mismatch"],
    "413": ["body_too_large"],
    "422": ["publish_invalid"],
    "428": ["precondition_required"],
    "429": ["rate_limited"],
    "500": ["internal_error"],
    "503": ["public_state_busy"],
  },
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

describe("resume surface", () => {
  it("declares the owner resume, publish, and PDF surface", () => {
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
    expect(version.description).toContain("unsupported_schema_version");

    const sectionKey = params.SectionKey.schema;
    expect(sectionKey.maxLength).toBe(36);
    expect(sectionKey.pattern).toBe("^[a-z]+$|^[0-9a-f-]{36}$");

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
      if (id === "downloadResumePDF" || id === "headResumePDF") {
        expect(refs).not.toContain(wanted);
        expect(refs).not.toContain("#/components/parameters/IfNoneMatch");
      } else if (id === "getResumePhoto") {
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
    const sourcePaths = [
      "packages/schema/resume.v1.schema.json",
      "packages/schema/resume.v2.schema.json",
    ];
    expect(patch.additionalProperties).toBe(false);
    expect(Object.keys(patch.properties)).toEqual([
      "fullName",
      "headline",
      "details",
    ]);
    for (const path of sourcePaths) {
      const source = JSON.parse(readFileSync(path, "utf8"));
      const personalDetails = source.$defs.personalDetails;
      expect(personalDetails.additionalProperties, path).toBe(false);
      expect(
        Object.keys(personalDetails.properties).filter(
          (name) => name !== "photo",
        ),
        path,
      ).toEqual(Object.keys(patch.properties));
    }

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
      const variants = doc.components.schemas[name].oneOf;
      expect(
        variants.length,
        `${name} is a closed command union`,
      ).toBeGreaterThan(1);
      expect(
        variants.every(
          (variant: { type: string; additionalProperties: boolean }) =>
            variant.type === "object" && variant.additionalProperties === false,
        ),
      ).toBe(true);
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

describe("Phase 5A publish and public wire contract", () => {
  it("defines the closed publish request and issue result", () => {
    const request = doc.components.schemas.PublishResumeRequest;
    expect(request.additionalProperties).toBe(false);
    expect(request.required).toEqual([
      "live",
      "downloadEnabled",
      "seoGeoEnabled",
    ]);
    expect(Object.keys(request.properties)).toEqual([
      "slug",
      "live",
      "downloadEnabled",
      "seoGeoEnabled",
    ]);
    expect(request.properties.slug).toMatchObject({
      type: "string",
      minLength: 1,
    });
    expect(request.properties.slug.default).toBeUndefined();
    expect(request.properties.slug.pattern).toBeUndefined();
    expect(request.properties.slug.maxLength).toBeUndefined();

    const issue = doc.components.schemas.PublishValidationIssue;
    expect(issue.additionalProperties).toBe(false);
    expect(issue.required).toEqual(["path", "code", "message"]);
    expect(issue.properties.code.enum).toEqual([
      "required_for_live",
      "requires_live",
      "invalid_format",
      "reserved",
      "required",
      "visible_entry_required",
    ]);
  });

  it("adds required owner flags without weakening owner/public separation", () => {
    const summary = doc.components.schemas.ResumeSummary;
    expect(summary.required).toEqual(
      expect.arrayContaining(["downloadEnabled", "seoGeoEnabled"]),
    );
    expect(summary.properties.downloadEnabled.type).toBe("boolean");
    expect(summary.properties.seoGeoEnabled.type).toBe("boolean");

    const publicResume = doc.components.schemas.PublicResume;
    expect(publicResume.additionalProperties).toBe(false);
    expect(publicResume.required).toEqual([
      "slug",
      "revision",
      "lng",
      "downloadEnabled",
      "document",
    ]);
    expect(Object.keys(publicResume.properties)).toEqual([
      "slug",
      "revision",
      "lng",
      "downloadEnabled",
      "document",
    ]);
    for (const ownerOnly of [
      "id",
      "title",
      "live",
      "seoGeoEnabled",
      "schemaVersion",
      "createdAt",
      "updatedAt",
    ]) {
      expect(publicResume.properties[ownerOnly]).toBeUndefined();
    }
  });

  it("keeps every public document leaf closed and omits private members", () => {
    const schemas = doc.components.schemas;
    const document = schemas.PublicResumeDocument;
    expect(document.additionalProperties).toBe(false);
    expect(document.required).toEqual([
      "schemaVersion",
      "personalDetails",
      "content",
      "customization",
    ]);
    expect(Object.keys(document.properties)).toEqual(document.required);

    const personal = schemas.PublicPersonalDetails;
    expect(personal.additionalProperties).toBe(false);
    expect(personal.required).toEqual(["fullName"]);
    expect(personal.properties.details.type).toBe("array");
    expect(personal.properties.details.nullable).toBeUndefined();
    expect(personal.properties.details.minItems).toBeUndefined();
    expect(personal.properties.details.items.$ref).toBe(
      "#/components/schemas/PublicPersonalDetail",
    );

    for (const name of [
      "PublicPersonalDetail",
      "PublicPhoto",
      "PublicPhotoCrop",
      "PublicYearMonth",
      "PublicDateRange",
      "PublicProfileEntry",
      "PublicWorkEntry",
      "PublicEducationEntry",
      "PublicSkillEntry",
      "PublicLanguageEntry",
      "PublicCertificateEntry",
      "PublicProjectEntry",
      "PublicCustomEntry",
      "PublicCustomization",
    ]) {
      expect(schemas[name], `${name} exists`).toBeTruthy();
      expect(schemas[name].additionalProperties, `${name} is closed`).toBe(
        false,
      );
    }
    for (const name of [
      "PublicPersonalDetail",
      "PublicProfileEntry",
      "PublicWorkEntry",
      "PublicEducationEntry",
      "PublicSkillEntry",
      "PublicLanguageEntry",
      "PublicCertificateEntry",
      "PublicProjectEntry",
      "PublicCustomEntry",
    ]) {
      expect(schemas[name].properties.isHidden, name).toBeUndefined();
    }
    expect(schemas.PublicPhoto.properties.key).toBeUndefined();
    expect(schemas.PublicPhoto.required).toEqual(["url"]);

    const exactLeafProperties: Record<string, string[]> = {
      PublicPersonalDetail: ["id", "label", "type", "value"],
      PublicPhoto: ["url", "crop"],
      PublicPhotoCrop: ["height", "width", "x", "y"],
      PublicYearMonth: ["m", "y"],
      PublicDateRange: ["end", "present", "start"],
      PublicProfileEntry: ["id", "text"],
      PublicWorkEntry: [
        "city",
        "country",
        "dates",
        "description",
        "employer",
        "employerLink",
        "id",
        "jobTitle",
      ],
      PublicEducationEntry: [
        "city",
        "country",
        "dates",
        "degree",
        "description",
        "id",
        "school",
        "schoolLink",
      ],
      PublicSkillEntry: ["id", "infoHtml", "level", "name"],
      PublicLanguageEntry: ["id", "level", "name"],
      PublicCertificateEntry: [
        "date",
        "description",
        "id",
        "issuer",
        "title",
        "titleLink",
      ],
      PublicProjectEntry: ["dates", "description", "id", "link", "title"],
      PublicCustomEntry: [
        "city",
        "dates",
        "description",
        "id",
        "subtitle",
        "title",
        "titleLink",
      ],
    };
    for (const [name, properties] of Object.entries(exactLeafProperties)) {
      expect(Object.keys(schemas[name].properties), name).toEqual(properties);
    }

    const content = schemas.PublicContent;
    expect(content.type).toBe("object");
    expect(content.maxProperties).toBe(24);
    expect(content.propertyNames).toMatchObject({
      maxLength: 36,
      pattern: "^[a-z]+$|^[0-9a-f-]{36}$",
    });
    expect(content.additionalProperties.$ref).toBe(
      "#/components/schemas/PublicSection",
    );
    const sections = schemas.PublicSection.oneOf;
    expect(sections).toHaveLength(8);
    expect(
      sections.map((section: any) => section.properties.sectionType.const),
    ).toEqual([
      "profile",
      "work",
      "education",
      "skill",
      "language",
      "certificate",
      "project",
      "custom",
    ]);
    expect(
      sections.every(
        (section: any) =>
          section.additionalProperties === false &&
          JSON.stringify(section).includes("Public") &&
          !JSON.stringify(section).includes("isHidden"),
      ),
    ).toBe(true);
  });

  it("declares JSON, photo, PDF, and share-image public operations", () => {
    const publicSurface = {
      "/public/resumes/{slug}": {
        get: "getPublicResume",
        head: "headPublicResume",
      },
      "/public/resumes/{slug}/photo": {
        get: "getPublicResumePhoto",
        head: "headPublicResumePhoto",
      },
      "/public/resumes/{slug}/pdf": {
        get: "getPublicResumePDF",
        head: "headPublicResumePDF",
      },
      "/public/resumes/{slug}/og.png": {
        get: "getPublicResumeShareImage",
        head: "headPublicResumeShareImage",
      },
    } as const;
    for (const [path, methods] of Object.entries(publicSurface)) {
      expect(Object.keys(doc.paths[path])).toEqual(Object.keys(methods));
      for (const [method, operationId] of Object.entries(methods)) {
        const operation = doc.paths[path][method];
        expect(operation.operationId).toBe(operationId);
        expect(operation.security).toEqual([]);
        expect(paramRefs(operation)).toEqual([
          "#/components/parameters/PublicSlug",
          "#/components/parameters/IfNoneMatch",
        ]);
        expect(Object.keys(operation.responses).sort()).toEqual([
          "200",
          "304",
          "400",
          "404",
          "405",
          ...(path.endsWith("/pdf") || path.endsWith("/og.png") ? ["429"] : []),
          "503",
        ]);
        const success = resolve(operation.responses["200"]);
        const wantedMedia = path.endsWith("/photo")
          ? ["image/jpeg", "image/png"]
          : path.endsWith("/pdf")
            ? ["application/pdf"]
            : path.endsWith("/og.png") ? ["image/png"] : ["application/json"];
        expect(Object.keys(success.content ?? {}).sort()).toEqual(wantedMedia);
        expect(Object.keys(success.headers ?? {}).sort()).toEqual([
          "Cache-Control",
          ...(path.endsWith("/pdf") ? ["Content-Disposition"] : []),
          "Content-Length",
          "ETag",
        ]);
        const notModified = resolve(operation.responses["304"]);
        expect(notModified.content).toBeUndefined();
        expect(Object.keys(notModified.headers ?? {}).sort()).toEqual([
          "Cache-Control",
          ...(path.endsWith("/pdf") ? ["Content-Disposition"] : []),
          "Content-Type",
          "ETag",
        ]);
        for (const [status, code] of [
          ["400", "request_invalid"],
          ["404", "public_not_found"],
          ["405", "method_not_allowed"],
          ["503", "temporarily_unavailable"],
        ] as const) {
          const response = resolve(operation.responses[status]);
          expect(codesIn(response), `${path} ${method} ${status}`).toContain(
            code,
          );
          expect(response.headers["Cache-Control"]).toBeTruthy();
          expect(response.headers.ETag).toBeUndefined();
        }
        expect(resolve(operation.responses["405"]).headers.Allow.example).toBe(
          "GET, HEAD",
        );
        expect(
          resolve(operation.responses["503"]).headers["Retry-After"].example,
        ).toBe(1);
      }
    }

    expect(doc.components.parameters.PublicSlug.schema).toMatchObject({
      type: "string",
      minLength: 4,
      maxLength: 30,
      pattern: "^[a-z0-9]+(-[a-z0-9]+)*$",
    });
    expect(doc.components.headers.PublicBodyETag.schema.pattern).toBe(
      '^"[0-9a-f]{64}"$',
    );
    for (const proseOnly of [
      "/{slug}",
      "/{slug}.md",
      "/sitemap.xml",
      "/robots.txt",
      "/llms.txt",
      "/internal-render/public",
      "/internal-render/print/redeem",
      "/print/{id}",
    ]) {
      expect(doc.paths[proseOnly]).toBeUndefined();
    }
  });
});

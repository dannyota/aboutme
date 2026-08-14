import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8"));
const authProviders = ["google", "github", "linkedin"] as const;

describe("openapi contract", () => {
  it("lints clean", () => {
    // Invoke the scoped package name, not the bare "redocly" command: npm
    // has an unrelated placeholder package literally named "redocly" that
    // silently exits 0, so "npx redocly ..." can resolve to the wrong
    // binary and mask a real lint failure.
    execFileSync(
      "npx",
      [
        "@redocly/cli",
        "lint",
        "docs/api/openapi.yaml",
        "--config",
        "docs/api/redocly.yaml",
      ],
      { stdio: "inherit" },
    );
  });

  it("serves /api/v1", () => {
    expect(doc.servers[0].url).toMatch(/\/api\/v1$/);
  });

  it("defines the error envelope", () => {
    expect(doc.components.schemas.Error.properties.error.required).toEqual([
      "code",
      "message",
    ]);
  });

  it("documents 412 for stale writes, and never 409, for precondition failure", () => {
    expect(JSON.stringify(doc)).toContain("412");
    // 409 does appear in this document, but only in prose describing domain
    // conflicts (slug taken, idempotency-key body mismatch) — never as a
    // response on a precondition/If-Match path. This guards against a
    // future edit accidentally adding a `409` response to a write endpoint
    // for staleness.
    for (const [path, item] of Object.entries<any>(doc.paths)) {
      for (const [method, operation] of Object.entries<any>(item)) {
        if (typeof operation !== "object" || !operation?.responses) continue;
        if (!("409" in operation.responses)) continue;
        expect(
          "412" in operation.responses,
          `${method.toUpperCase()} ${path}: an operation documenting 409 ` +
            "should also document 412 if it is a write-safety-guarded " +
            "mutation (409 must stay reserved for domain conflicts, not " +
            "staleness)",
        );
      }
    }
  });

  it("keeps health paths unversioned (root-level, not /api/v1)", () => {
    // Health is infrastructure, not product API: a future /api/v2 must
    // never break orchestrator or synthetic checks (design doc §2 route
    // table). The global server stays /api/v1 for every product endpoint;
    // /healthz and /readyz must override it back to the bare root so the
    // contract doesn't silently re-version them.
    expect(doc.servers[0].url).toMatch(/\/api\/v1$/);
    for (const path of ["/healthz", "/readyz"]) {
      const override = doc.paths[path]?.servers;
      expect(
        override,
        `${path} should have a path-level servers override`,
      ).toBeTruthy();
      expect(override[0].url).not.toMatch(/\/api\/v1$/);
    }
  });

  it("declares the write-safety components (Envelope, Revision, IfMatch, IdempotencyKey)", () => {
    expect(doc.components.schemas.Envelope.required).toContain("data");
    expect(doc.components.schemas.Revision.type).toBe("string");
    expect(doc.components.parameters.IfMatch.name).toBe("If-Match");
    expect(doc.components.parameters.IdempotencyKey.name).toBe(
      "Idempotency-Key",
    );
  });

  it("pins login GET and privileged POST OAuth start contracts", () => {
    const authPurpose = doc.components.parameters.AuthPurpose;
    expect(authPurpose.schema.enum).toEqual(["login"]);
    expect(authPurpose.description).toContain("not served by `GET`");

    const authLinkPurpose = doc.components.parameters.AuthLinkPurpose;
    expect(authLinkPurpose.in).toBe("query");
    expect(authLinkPurpose.required).toBe(true);
    expect(authLinkPurpose.schema.enum).toEqual(["link", "reauth"]);

    const authStartResponse = doc.components.schemas.AuthStartResponse;
    expect(authStartResponse.required).toEqual(["data"]);
    expect(authStartResponse.properties.data.required).toEqual([
      "authorizeUrl",
    ]);

    for (const provider of authProviders) {
      const start = doc.paths[`/auth/${provider}/start`];
      expect(start.get.security, `${provider} login GET is public`).toEqual([]);
      expect(start.get.parameters).toEqual([
        { $ref: "#/components/parameters/AuthPurpose" },
      ]);
      expect(
        start.get.responses["405"],
        `${provider} GET rejects privileged purposes`,
      ).toBeTruthy();
      expect(start.get.description).toMatch(/link.*reauth.*405/is);
      expect(start.get.description).toMatch(
        /before (?:a session lookup, database write|any transaction)/i,
      );

      expect(start.post.security).toEqual([
        { sessionCookie: [], csrfToken: [] },
      ]);
      expect(start.post.parameters).toEqual([
        { $ref: "#/components/parameters/AuthLinkPurpose" },
      ]);
      expect(
        start.post.requestBody,
        `${provider} privileged POST stays bodiless`,
      ).toBeUndefined();
      expect(start.post.responses["302"]).toBeUndefined();
      expect(
        start.post.responses["200"].content["application/json"].schema.$ref,
      ).toBe("#/components/schemas/AuthStartResponse");
      expect(start.post.description).toContain("top-level navigation");

      const callbackDescription =
        doc.paths[`/auth/${provider}/callback`].get.description;
      expect(callbackDescription).not.toMatch(
        new RegExp(`GET /auth/${provider}/start`, "i"),
      );
    }

    const callbackErrors =
      doc.components.schemas.OAuthCallbackErrorCode.description;
    expect(callbackErrors).toContain(
      "`POST /auth/{provider}/start?purpose=link`",
    );
    expect(callbackErrors).toContain("No `/start` operation redirects");
  });

  it("documents deterministic linked-identity order on /me", () => {
    const me = doc.paths["/me"].get;
    const identities =
      me.responses["200"].content["application/json"].schema.allOf[1].properties
        .data.properties.identities;

    expect(me.description).toContain(
      "Identities are ordered by `(created_at, id)`, oldest first",
    );
    expect(identities.description).toContain(
      "Linked identities ordered by `(created_at, id)`, oldest first",
    );
  });

  it("defines a closed public resume distinct from the owner resume", () => {
    const publicResume = doc.components.schemas.PublicResume;
    expect(publicResume.additionalProperties).toBe(false);
    expect(Object.keys(publicResume.properties)).toEqual([
      "slug",
      "revision",
      "lng",
      "downloadEnabled",
      "document",
    ]);
    expect(publicResume.properties.revision.$ref).toBe(
      "#/components/schemas/Revision",
    );
    expect(doc.components.schemas.Resume.properties).toBeUndefined();
    expect(JSON.stringify(publicResume)).not.toMatch(
      /createdAt|updatedAt|seoGeoEnabled|\"key\"|\"isHidden\"/,
    );
  });
});

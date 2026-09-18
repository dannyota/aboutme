import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8")) as any;

describe("DELETE /me/identities/{identityId}", () => {
  const path = doc.paths["/me/identities/{identityId}"];
  const op = path?.delete;

  it("is the only method, cookie-authenticated with CSRF", () => {
    expect(op?.operationId).toBe("deleteMeIdentity");
    expect(Object.keys(path).sort()).toEqual(["delete", "parameters"]);
    expect(op.security).toEqual([{ sessionCookie: [], csrfToken: [] }]);
    expect(path.parameters).toEqual([
      { $ref: "#/components/parameters/IdentityID" },
    ]);
  });

  it("documents the closed status set and error codes", () => {
    expect(Object.keys(op.responses).sort()).toEqual([
      "204",
      "401",
      "403",
      "404",
      "405",
      "409",
      "429",
      "500",
    ]);
    expect(
      op.responses["409"].content["application/json"].example.error.code,
    ).toBe("last_sign_in_method");
    expect(
      op.responses["404"].content["application/json"].example.error.code,
    ).toBe("not_found");
    expect(
      op.responses["403"].content["application/json"].examples.reauth_required
        .value.error.code,
    ).toBe("reauth_required");
  });
});

describe("GET /me identities", () => {
  it("carry the id DELETE takes, the provider, and when it was linked", () => {
    const identity = doc.components.schemas.Identity;
    expect(identity.required.sort()).toEqual(["createdAt", "id", "provider"]);
    expect(identity.properties.id.format).toBe("uuid");
    expect(identity.properties.createdAt.format).toBe("date-time");
    expect(identity.properties).not.toHaveProperty("providerUserId");
  });
});

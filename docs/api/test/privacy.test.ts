import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8"));

describe("account privacy contract", () => {
  it("keeps account export cookie-only and deletion CSRF-protected", () => {
    const exported = doc.paths["/me/export"]?.get;
    const deleted = doc.paths["/me"]?.delete;
    expect(exported?.operationId).toBe("getAccountExport");
    expect(deleted?.operationId).toBe("deleteAccount");
    expect(exported?.security).toEqual([{ sessionCookie: [] }]);
    expect(deleted?.security).toEqual([{ sessionCookie: [], csrfToken: [] }]);
    for (const operation of [exported, deleted]) {
      expect(operation.requestBody).toBeUndefined();
      expect(operation.parameters ?? []).toEqual([]);
      expect(operation.responses["405"]).toBeDefined();
      expect(operation.responses["429"]).toBeDefined();
      expect(operation.responses["503"]).toBeDefined();
    }
    expect(deleted.responses["204"].content).toBeUndefined();
    expect(deleted.responses["204"].headers["Clear-Site-Data"]).toBeDefined();
  });

  it("closes the portable envelope and excludes credential fields", () => {
    const schemas = doc.components.schemas;
    expect(schemas.AccountExport?.additionalProperties).toBe(false);
    const bundle = schemas.AccountExport?.properties.data;
    expect(bundle?.additionalProperties).toBe(false);
    expect(bundle?.required).toEqual([
      "exportVersion",
      "exportedAt",
      "account",
      "resumes",
    ]);
    expect(bundle?.properties.exportVersion.const).toBe(1);
    expect(bundle?.properties.resumes.maxItems).toBe(3);
    expect(schemas.AccountExportProfile?.required).toEqual([
      "id",
      "email",
      "name",
      "createdAt",
      "updatedAt",
      "linkedProviders",
    ]);
    expect(Object.keys(schemas.AccountExportProfile?.properties ?? {})).toEqual(
      schemas.AccountExportProfile?.required,
    );
    expect(schemas.AccountExportPhoto?.anyOf[1].properties.data.maxLength).toBe(
      2796204,
    );
    expect(
      schemas.AccountExportPhoto?.anyOf[1].properties.mediaType.enum,
    ).toEqual(["image/jpeg", "image/png"]);
    expect(
      schemas.AccountExportDocument?.allOf[1].properties.personalDetails
        .properties.photo,
    ).toBe(false);
  });

  it("declares a fixed private attachment and current schema header", () => {
    const response = doc.paths["/me/export"]?.get.responses["200"];
    expect(response?.headers["Content-Disposition"].schema.const).toBe(
      'attachment; filename="aboutme-export.json"',
    );
    expect(response?.headers["Cache-Control"].schema.const).toBe(
      "no-store, no-transform",
    );
    expect(response?.headers["X-Resume-Schema-Version"]).toBeDefined();
    expect(response?.content["application/json"].schema.$ref).toBe(
      "#/components/schemas/AccountExport",
    );
  });
});

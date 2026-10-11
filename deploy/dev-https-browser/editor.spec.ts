import { expect, test, type Page } from '@playwright/test';
import { deleteRecordedResume, loginAsDevelopmentUser } from './editor-fixtures';
import { pinEnglish } from './harness-lib';
import { proveListLoadAutosave } from './editor-autosave';
import {
  proveKeyboardStructureAndContextActions,
  deleteThroughListKeyboard,
} from './editor-structure';
import { proveConflictAndTemplate } from './editor-conflicts';
import { provePhotoSessionPersistence } from './editor-photo-session';
import { proveAccessibility } from './editor-accessibility';
import { installDiagnostics } from './editor-diagnostics';
import {
  classifyLocaleFailure,
  type LocaleScenarioState,
  installLocaleDiagnostics,
  proveLocaleDraft,
} from './editor-locale';
import {
  type EditorProofState,
  prepareEditor,
  type EditorSteps,
  ensureAuthenticated,
  writeEditorEvidence,
} from './editor-proof-state';

const state: EditorProofState = {
  editorDiagnosticStage: 'setup',
  localeDiagnosticStage: 'setup',
  recordedResumeIDs: new Set<string>(),
};

test.afterEach(async ({ context }, testInfo) => {
  testInfo.setTimeout(testInfo.timeout + 30_000);
  const stage = testInfo.title === 'keeps a date draft focused when the interface locale changes'
    ? state.localeDiagnosticStage
    : state.editorDiagnosticStage;
  try {
    if (state.recordedResumeIDs.size > 0) {
      const cleanup = await context.newPage();
      try {
        await ensureAuthenticated(cleanup);
        for (const id of state.recordedResumeIDs) {
          await deleteRecordedResume(cleanup, id);
          state.recordedResumeIDs.delete(id);
        }
      } finally {
        await cleanup.close();
      }
    }
  } catch (error) {
    process.stderr.write('editor-stage:cleanup\n');
    throw error;
  }
  if (testInfo.status !== testInfo.expectedStatus) {
    process.stderr.write(`editor-stage:${stage}\n`);
  }
});

test('proves authenticated editor behavior over trusted HTTPS', async ({
  context,
  page,
}) => {
  state.editorDiagnosticStage = 'setup';
  await prepareEditor(state, () => pinEnglish(context));
  const steps: EditorSteps = {
    auth: false,
    cache: false,
    etag: false,
    ifMatch: false,
    autosave: false,
    conflict: false,
    template: false,
    photo: false,
    session: false,
    persistence: false,
    accessibility: false,
    teardown: false,
  };
  const diagnostics = await prepareEditor(state, () => installDiagnostics(state, context, page));
  let reauthPage: Page | undefined;
  let scenarioSucceeded = false;

  try {
    await loginAsDevelopmentUser(page);
    steps.auth = true;
    const scenario = await proveListLoadAutosave(
      state,
      page,
      context,
      state.recordedResumeIDs,
      steps,
    );
    await proveKeyboardStructureAndContextActions(
      state,
      page,
      scenario.accepted.metadata.id,
      diagnostics,
      scenario.probes,
      scenario.baseline,
    );
    state.editorDiagnosticStage = 'conflict-template';
    await proveConflictAndTemplate(
      state,
      page,
      scenario.accepted,
      diagnostics,
      scenario.probes,
      scenario.baseline,
    );
    steps.conflict = true;
    steps.template = true;
    state.editorDiagnosticStage = 'photo-session';
    reauthPage = await provePhotoSessionPersistence(
      state,
      page,
      context,
      scenario.accepted.metadata.id,
      diagnostics,
      scenario.probes,
      scenario.baseline,
    );
    steps.photo = true;
    steps.session = true;
    steps.persistence = true;
    state.editorDiagnosticStage = 'accessibility';
    await proveAccessibility(state, page, scenario.accepted.metadata.id);
    steps.accessibility = true;
    state.editorDiagnosticStage = 'teardown';
    await deleteThroughListKeyboard(state, page, scenario.accepted.metadata.id);
    state.recordedResumeIDs.delete(scenario.accepted.metadata.id);
    steps.teardown = true;
    scenarioSucceeded = true;
  } catch (error) {
    const sourceLine = error instanceof Error
      ? /editor\.spec\.ts:([0-9]{1,4}):/u.exec(error.stack ?? '')?.[1]
      : undefined;
    if (sourceLine !== undefined) {
      state.editorDiagnosticStage = `${state.editorDiagnosticStage}-line-${sourceLine}`;
    }
    throw error;
  } finally {
    try {
      if (!scenarioSucceeded) {
        process.stderr.write(`editor-stage:${state.editorDiagnosticStage}\n`);
      }
      if (reauthPage !== undefined && !reauthPage.isClosed()) await reauthPage.close();
    } catch (error) {
      state.editorDiagnosticStage = 'cleanup';
      process.stderr.write(`editor-stage:${state.editorDiagnosticStage}\n`);
      throw error;
    }
  }

  try {
    state.editorDiagnosticStage = 'post-scenario';
    expect(scenarioSucceeded).toBe(true);
    state.editorDiagnosticStage = 'post-diagnostics';
    diagnostics.assertClean();
    state.editorDiagnosticStage = 'post-steps';
    expect(steps).toEqual({
      auth: true,
      cache: true,
      etag: true,
      ifMatch: true,
      autosave: true,
      conflict: true,
      template: true,
      photo: true,
      session: true,
      persistence: true,
      accessibility: true,
      teardown: true,
    });
    state.editorDiagnosticStage = 'post-evidence';
    await writeEditorEvidence(steps);
  } catch (error) {
    process.stderr.write(`editor-stage:${state.editorDiagnosticStage}\n`);
    throw error;
  }
});

test('keeps a date draft focused when the interface locale changes', async ({
  context,
  page,
}) => {
  state.localeDiagnosticStage = 'setup';
  const localeSetup = async <T>(operation: () => Promise<T>): Promise<T> => {
    try {
      return await operation();
    } catch (error) {
      process.stderr.write(`editor-stage:${state.localeDiagnosticStage}\n`);
      throw error;
    }
  };
  await localeSetup(() => pinEnglish(context));
  const locale: LocaleScenarioState = {
    viewport: 'setup',
    target: 'en',
    resumeID: undefined,
    writes: 0,
  };
  const counters = await installLocaleDiagnostics(context, page, localeSetup, locale);
  try {
    await proveLocaleDraft(page, state, locale);
  } catch (error) {
    state.localeDiagnosticStage = `${state.localeDiagnosticStage}-${locale.viewport}-${locale.target}-${classifyLocaleFailure(error)}`;
    const sourceLine = error instanceof Error
      ? /editor\.spec\.ts:([0-9]{1,4}):/u.exec(error.stack ?? '')?.[1]
      : undefined;
    if (sourceLine !== undefined) {
      state.localeDiagnosticStage = `${state.localeDiagnosticStage}-line-${sourceLine}`;
    }
    process.stderr.write(`editor-stage:${state.localeDiagnosticStage}\n`);
    throw error;
  }
  try {
    expect(counters).toEqual({
      certificateErrors: 0,
      consoleErrors: 0,
      externalRequests: 0,
      pageErrors: 0,
    });
  } catch (error) {
    process.stderr.write(`editor-stage:${state.localeDiagnosticStage}\n`);
    throw error;
  }
});

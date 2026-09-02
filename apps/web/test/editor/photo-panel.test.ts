import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import CropEditor from '../../app/components/editor/photo/CropEditor.vue';
import PhotoPanel from '../../app/components/editor/photo/PhotoPanel.vue';
import type {
  ResumeEditorActions,
} from '../../app/composables/useResumeEditor';
import type { AtomicConflictRecord } from '../../app/editor/conflicts';
import type { AtomicEditorCommand } from '../../app/editor/commands';
import type { Projection } from '../../app/editor/types';
import type { ResumeRecord } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

describe('private photo controls', () => {
  it('shows the frame without an image until the read is ready', () => {
    const wrapper = mount(PhotoPanel, {
      props: {
        record: photoRecord({
          photoRead: { kind: 'loading', binding: 'k', generation: 1 },
        }),
        actions: actionsFor(vi.fn()),
      },
    });
    const preview = wrapper.get('[data-photo-preview]');
    expect(preview.find('[data-photo-image]').exists()).toBe(false);
    expect(preview.find('.size-32').exists()).toBe(true);
    expect(preview.find('svg').exists()).toBe(true);
    expect(preview.text()).toContain('Photo preview is loading.');
  });

  it.each([
    ['keep-observed', undefined],
    ['replace', new File(['new'], 'new.png', { type: 'image/png' })],
  ] as const)('resolves a cutoff with %s', async (kind, file) => {
    const resolveOpaquePhoto = vi.fn();
    const actions = { resolveOpaquePhoto } as unknown as ResumeEditorActions;
    const wrapper = mount(PhotoPanel, {
      props: { record: cutoffRecord(), actions },
    });

    if (file) await selectFile(wrapper.get('input[type="file"]'), file);
    await wrapper.get(`[data-action="${kind}"]`).trigger('click');

    expect(resolveOpaquePhoto).toHaveBeenCalledWith(
      'photo-upload-1',
      file ? { kind, file } : { kind },
    );
  });

  it('keeps selected source bytes opaque while forwarding the original File',
    async () => {
      const edit = vi.fn();
      const reader = vi.fn();
      const image = vi.fn();
      const objectUrl = vi.fn();
      const canvas = vi.spyOn(HTMLCanvasElement.prototype, 'getContext');
      const storage = vi.spyOn(Storage.prototype, 'setItem');
      vi.stubGlobal('FileReader', reader);
      vi.stubGlobal('Image', image);
      vi.stubGlobal('URL', { createObjectURL: objectUrl });
      const wrapper = mount(PhotoPanel, {
        props: { record: photoRecord(), actions: actionsFor(edit) },
      });
      const file = new File(['source bytes'], 'source.png', {
        type: 'image/png',
      });

      await selectFile(wrapper.get('input[type="file"]'), file);

      expect(edit).toHaveBeenCalledWith({ kind: 'photoUpload', file });
      expect(edit.mock.calls[0]![0].file).toBe(file);
      expect(reader).not.toHaveBeenCalled();
      expect(image).not.toHaveBeenCalled();
      expect(objectUrl).not.toHaveBeenCalled();
      expect(canvas).not.toHaveBeenCalled();
      expect(storage).not.toHaveBeenCalled();
      expect(wrapper.find('img').exists()).toBe(false);
      expect(wrapper.text()).not.toContain('source.png');
      vi.unstubAllGlobals();
      canvas.mockRestore();
      storage.mockRestore();
    },
  );

  it('requires current-photo confirmation before deletion', async () => {
    const edit = vi.fn();
    const wrapper = mount(PhotoPanel, {
      props: { record: photoRecord(), actions: actionsFor(edit) },
    });

    await wrapper.get('[data-action="delete"]').trigger('click');
    expect(edit).not.toHaveBeenCalled();
    const confirm = document.body.querySelector(
      '[data-action="confirm-delete"]',
    ) as HTMLElement;
    confirm.click();

    expect(edit).toHaveBeenCalledWith({ kind: 'photoDelete' });
  });

  it('contains the delete dialog and restores its opener after cancellation',
    async () => {
      const wrapper = mount(PhotoPanel, {
        attachTo: document.body,
        props: { record: photoRecord(), actions: actionsFor(vi.fn()) },
      });
      const opener = wrapper.get('[data-action="delete"]');
      (opener.element as HTMLButtonElement).focus();

      await opener.trigger('click');
      await nextTick();
      const dialog = document.body.querySelector(
        '[role="alertdialog"]',
      ) as HTMLElement;
      expect(dialog.getAttribute('role')).toBe('alertdialog');
      expect(dialog.getAttribute('aria-labelledby')).toBeTruthy();
      expect(dialog.getAttribute('aria-describedby')).toBeTruthy();
      expect(document.activeElement).toBe(
        document.body.querySelector('[data-action="cancel-delete"]'),
      );
      dialog.dispatchEvent(new KeyboardEvent('keydown', {
        bubbles: true,
        key: 'Escape',
      }));
      await nextTick();
      await nextTick();
      await nextTick();

      expect(document.body.querySelector('[role="alertdialog"]')).toBeNull();
      expect(document.activeElement).toBe(opener.element);
      wrapper.unmount();
    },
  );

  it('reconfirms deletion when the current photo changes', async () => {
    const edit = vi.fn();
    const wrapper = mount(PhotoPanel, {
      attachTo: document.body,
      props: { record: photoRecord(), actions: actionsFor(edit) },
    });
    const opener = wrapper.get('[data-action="delete"]');
    (opener.element as HTMLButtonElement).focus();
    await opener.trigger('click');
    await wrapper.setProps({ record: photoRecordForKey('photo-b') });
    const confirm = document.body.querySelector(
      '[data-action="confirm-delete"]',
    ) as HTMLElement;
    confirm.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await nextTick();
    await nextTick();
    await nextTick();

    expect(edit).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain(
      'This photo changed. Reopen deletion and confirm again.',
    );
    expect(document.activeElement).toBe(opener.element);
    wrapper.unmount();
  });

  it.each([
    [
      'busy', retryRecord('media-busy', 3_000),
      'Photo processing is busy. Try again in 3 seconds.',
    ],
    [
      'rate', retryRecord('rate-limited', 2_000),
      'Too many photo requests. Try again in 2 seconds.',
    ],
    [
      'network', failedRecord('unknown'),
      'We could not confirm the photo request.',
    ],
    [
      'revision', failedRecord('precondition_required'),
      'The photo changed. Refresh and try again.',
    ],
    [
      'session', photoRecord({ sessionLost: true }),
      'Your session ended. Sign in to continue.',
    ],
  ] as const)('shows safe %s status text', (_, record, message) => {
    const wrapper = mount(PhotoPanel, {
      props: { record, actions: actionsFor(vi.fn()) },
    });

    expect(wrapper.text()).toContain(message);
    expect(wrapper.text()).not.toContain('photo-a');
  });

  it('suspends preview while replacement and deletion remain usable', () => {
    const wrapper = mount(PhotoPanel, {
      props: {
        record: photoRecord({
          photoRead: {
            kind: 'suspended', binding: 'photo-a', generation: 1,
            reason: 'read-failed',
          },
        }),
        actions: actionsFor(vi.fn()),
      },
    });

    expect(wrapper.find('img').exists()).toBe(false);
    expect(wrapper.get('[data-photo-preview]').text()).toContain(
      'Photo preview is unavailable.',
    );
    expect(
      wrapper.get('input[type="file"]').attributes('disabled'),
    ).toBeUndefined();
    expect(
      wrapper.get('[data-action="delete"]').attributes('disabled'),
    ).toBeUndefined();
  });

  it('updates the optimistic rectangle from pointer and keyboard crop input',
    async () => {
      const edit = vi.fn();
      const wrapper = mount(CropEditor, {
        props: {
          photoKey: 'photo-a',
          photoUrl: 'data:image/png;base64,accepted',
          crop: { x: 0, y: 0, width: 0.5, height: 0.5 },
          actions: actionsFor(edit),
        },
      });
      const target = wrapper.get('[data-crop-stage]');
      const rectangle = wrapper.get('[data-crop-rectangle]');
      Object.defineProperty(target.element, 'getBoundingClientRect', {
        value: () => ({ left: 0, top: 0, width: 100, height: 100 }),
      });

      await target.trigger('pointerdown', { clientX: 10, clientY: 20 });
      expect(rectangle.attributes('style')).toContain('left: 10%;');
      await target.trigger('pointermove', { clientX: 20, clientY: 20 });
      expect(rectangle.attributes('style')).toContain('left: 20%;');
      await target.trigger('keydown', { key: 'ArrowRight' });
      expect(rectangle.attributes('style')).toContain('left: 25%;');
      await wrapper.get('form').trigger('submit');

      expect(edit).toHaveBeenCalledWith({
        kind: 'photoCrop',
        crop: { x: 0.25, y: 0.2, width: 0.5, height: 0.5 },
      });
    },
  );

  it('reopens a changed-photo crop conflict without offering generic override',
    async () => {
      const acceptLatest = vi.fn();
      const wrapper = mount(PhotoPanel, {
        props: {
          record: photoRecord({
            conflicts: [photoChangedConflict()],
          }),
          actions: { acceptLatest } as unknown as ResumeEditorActions,
        },
      });

      expect(wrapper.find('[data-action="apply-mine"]').exists()).toBe(false);
      await wrapper.get('[data-action="reopen-crop"]').trigger('click');
      expect(acceptLatest).toHaveBeenCalledWith('crop-conflict');
    },
  );

  it('rejects blank crop values instead of treating them as zero', async () => {
    const edit = vi.fn();
    const wrapper = mount(CropEditor, {
      props: {
        photoKey: 'photo-a',
        photoUrl: 'data:image/png;base64,accepted',
        actions: actionsFor(edit),
      },
    });

    await wrapper.get('[name="x"]').setValue('  ');
    await wrapper.get('form').trigger('submit');

    expect(edit).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain('Enter a crop within the image bounds.');
  });

  it.each([
    ['negative x', 'x', '-0.01'],
    ['negative y', 'y', '-0.01'],
    ['zero width', 'width', '0'],
    ['zero height', 'height', '0'],
    ['wide rectangle', 'width', '1.01'],
    ['tall rectangle', 'height', '1.01'],
    ['x overflow', 'x', '0.01'],
    ['y overflow', 'y', '0.01'],
  ] as const)('rejects a crop with %s', async (_, field, value) => {
    const edit = vi.fn();
    const wrapper = mount(CropEditor, {
      props: {
        photoKey: 'photo-a',
        photoUrl: 'data:image/png;base64,accepted',
        actions: actionsFor(edit),
      },
    });

    await wrapper.get(`[name="${field}"]`).setValue(value);
    await wrapper.get('form').trigger('submit');

    expect(edit).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain('Enter a crop within the image bounds.');
  });

  it('accepts exact crop boundaries and clears the accepted crop', async () => {
    const edit = vi.fn();
    const wrapper = mount(CropEditor, {
      props: {
        photoKey: 'photo-a',
        photoUrl: 'data:image/png;base64,accepted',
        crop: { x: 0.25, y: 0.25, width: 0.5, height: 0.5 },
        actions: actionsFor(edit),
      },
    });

    for (const [field, value] of [
      ['x', '0'],
      ['y', '0'],
      ['width', '1'],
      ['height', '1'],
    ] as const) {
      await wrapper.get(`[name="${field}"]`).setValue(value);
    }
    await wrapper.get('form').trigger('submit');
    await wrapper.get('[data-action="clear-crop"]').trigger('click');

    expect(edit.mock.calls).toEqual([
      [{ kind: 'photoCrop', crop: { x: 0, y: 0, width: 1, height: 1 } }],
      [{ kind: 'photoCrop', crop: null }],
    ]);
  });

  it('submits a second crop after the accepted crop changes', async () => {
    const edit = vi.fn();
    const wrapper = mount(CropEditor, {
      props: {
        photoKey: 'photo-a',
        photoUrl: 'data:image/png;base64,accepted',
        crop: { x: 0, y: 0, width: 1, height: 1 },
        actions: actionsFor(edit),
      },
    });

    await wrapper.setProps({
      crop: { x: 0, y: 0, width: 0.75, height: 1 },
    });
    await wrapper.get('[name="width"]').setValue('0.5');
    await wrapper.get('form').trigger('submit');

    expect(edit).toHaveBeenCalledWith({
      kind: 'photoCrop',
      crop: { x: 0, y: 0, width: 0.5, height: 1 },
    });
  });

  it('offers retry only for a valid retryable photo command', async () => {
    const retry = vi.fn();
    const wrapper = mount(PhotoPanel, {
      props: {
        record: retryRecord('media-busy', 1_000),
        actions: { retry } as unknown as ResumeEditorActions,
      },
    });

    await wrapper.get('[data-action="retry-photo"]').trigger('click');

    expect(retry).toHaveBeenCalledWith('retry-photo-1');
  });

  it('offers retry for a retryable photo delete', async () => {
    const retry = vi.fn();
    const wrapper = mount(PhotoPanel, {
      props: {
        record: retryRecord('rate-limited', 1_000, 'photoDelete'),
        actions: { retry } as unknown as ResumeEditorActions,
      },
    });

    await wrapper.get('[data-action="retry-photo"]').trigger('click');

    expect(retry).toHaveBeenCalledWith('retry-photo-1');
  });

  it('labels a dispatching photo upload without claiming completion', () => {
    const wrapper = mount(PhotoPanel, {
      props: {
        record: photoRecord({
          attempt: {
            kind: 'dispatching',
            command: { kind: 'photoUpload' },
          } as never,
        }),
        actions: actionsFor(vi.fn()),
      },
    });

    expect(wrapper.text()).toContain('Uploading photo.');
    expect(wrapper.text()).not.toContain('uploaded');
  });

  it('shows only cutoff decisions while a photo outcome is opaque', () => {
    const wrapper = mount(PhotoPanel, {
      props: { record: cutoffRecord(), actions: actionsFor(vi.fn()) },
    });

    const actions = wrapper
      .findAll('[data-action]')
      .map((node) => node.attributes('data-action'));
    expect(wrapper.findAll('button:not([data-slot="button"])')).toHaveLength(0);
    expect(actions).toEqual([
      'upload-photo-input',
      'keep-observed',
      'replace',
    ]);
    expect(wrapper.find('[data-crop-stage]').exists()).toBe(false);
  });
});

function actionsFor(edit: ReturnType<typeof vi.fn>): ResumeEditorActions {
  return { edit } as unknown as ResumeEditorActions;
}

function photoRecord(overrides: Partial<ResumeRecord> = {}): ResumeRecord {
  return photoRecordForKey('photo-a', overrides);
}

function photoRecordForKey(
  key: string,
  overrides: Partial<ResumeRecord> = {},
): ResumeRecord {
  const accepted = withPhoto(acceptedFixture(), key);
  return {
    accepted,
    current: { document: accepted.document, metadata: accepted.metadata },
    pending: [], attempt: null, conflicts: [], issues: {}, templateState: null,
    photoRead: { kind: 'none' }, completeReadRequired: false,
    sessionLost: false, opaquePhotoOutcome: null,
    ...overrides,
  };
}

function withPhoto(value: ResumeRecord['accepted'], key: string) {
  return {
    ...value,
    document: {
      ...value.document,
      personalDetails: { ...value.document.personalDetails, photo: { key } },
    },
  };
}

function cutoffRecord(): ResumeRecord {
  return photoRecord({
    opaquePhotoOutcome: {
      kind: 'photo-cutoff',
      command: { id: 'photo-upload-1', kind: 'photoUpload' } as never,
      attempt: {} as never,
      observed: 'unavailable',
    },
  });
}

function retryRecord(
  reason: 'media-busy' | 'rate-limited',
  retryAfterMs: number,
  kind: 'photoUpload' | 'photoDelete' | 'photoCrop' = 'photoUpload',
): ResumeRecord {
  return photoRecord({
    attempt: {
      kind: 'retry-later',
      reason,
      retryAfterMs,
      command: { id: 'retry-photo-1', kind },
    } as never,
  });
}

function photoChangedConflict(): AtomicConflictRecord {
  const record = photoRecord();
  return {
    id: 'crop-conflict',
    subject: 'atomic',
    kind: 'photo-changed',
    command: { kind: 'photoCrop' } as AtomicEditorCommand,
    latest: record.current,
    latestProjection: {
      target: { present: false },
      context: {},
    } as Projection,
  };
}

function failedRecord(
  reason: 'unknown' | 'precondition_required',
): ResumeRecord {
  return photoRecord({
    attempt: reason === 'unknown'
      ? { kind: 'unknown' }
      : { kind: 'failed', reason },
  } as never);
}

async function selectFile(
  input: { readonly element: Element; trigger(event: string): Promise<void> },
  file: File,
): Promise<void> {
  Object.defineProperty(input.element, 'files', {
    configurable: true,
    value: { item: (index: number) => index === 0 ? file : null },
  });
  await input.trigger('change');
}

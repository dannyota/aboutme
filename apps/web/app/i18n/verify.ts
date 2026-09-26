// The verify page (/verify) in both site languages. Copy follows
// docs/design/deployment-transparency/page.md and visual.md.
import type {
  CheckStatus,
  ComponentName,
  DeploymentDocument,
  Failure,
} from '../utils/deploymentDocument';
import type { ChipKind, ConnectorKind } from '../utils/verifyView';
import type { Locale } from './locale';

export const githubCliManualUrl
  = 'https://cli.github.com/manual/gh_attestation_verify';
export const limitsUrl = 'https://github.com/dannyota/aboutme/blob/main/'
  + 'docs/design/deployment-transparency/README.md#what-this-proves';
export const sourceRepositoryName = 'dannyota/aboutme';
export const sourceRepositoryUrl = 'https://github.com/dannyota/aboutme';
export const sbomPredicateFlag
  = '--predicate-type https://spdx.dev/Document/v2.3';

export type ChainStep = 'source' | 'build' | 'image' | 'running';

export interface VerifyCopy {
  readonly title: string;
  readonly description: string;
  readonly lead: string;
  readonly statusHeading: string;
  readonly loading: string;
  readonly unavailable: { readonly title: string; readonly detail: string };
  readonly outdated: {
    readonly title: string;
    readonly detail: string;
    readonly reload: string;
  };
  readonly stale: {
    readonly title: (time: string) => string;
    readonly detail: (age: string) => string;
    readonly pill: (version: string) => string;
  };
  readonly mismatch: {
    readonly title: string;
    readonly detail: Readonly<
      Record<Failure['reason'], (component: ComponentName) => string>
    >;
    readonly summary: string;
    readonly more: (count: number) => string;
  };
  readonly unverified: {
    readonly title: string;
    readonly detail: (component: ComponentName) => string;
  };
  readonly rollingOut: {
    readonly title: string;
    readonly detail: (component: ComponentName, from: string, to: string)
    => string;
  };
  readonly verified: {
    readonly title: string;
    readonly detail: (count: number, version: string, commit: string)
    => string;
    /** Signed builds of different versions, so there is no one release. */
    readonly detailMixed: string;
  };
  readonly checkedAt: (time: string) => string;
  readonly lastCheckedAt: (time: string) => string;
  readonly refreshes: string;
  readonly ago: (span: string) => string;
  readonly justNow: string;
  readonly span: (seconds: number) => string;
  readonly chain: {
    readonly heading: string;
    readonly hint: string;
    readonly steps: Readonly<Record<ChainStep, string>>;
    readonly buildValue: string;
    readonly buildLine: (runId: string) => string;
    readonly imageLine: (count: number) => string;
    readonly runningLine: (place: string, replicas: number, time: string)
    => string;
    readonly chips: Readonly<Record<Exclude<ChipKind, 'failed'>, string>>;
    readonly failed: Readonly<
      Record<Failure['reason'], (component: ComponentName) => string>
    >;
    readonly connectors: Readonly<Record<ConnectorKind, string>>;
  };
  readonly components: {
    readonly heading: string;
    readonly rollingHint: string;
    readonly roles: Readonly<Record<ComponentName, string>>;
    readonly versionUnknown: string;
    readonly older: string;
    readonly newest: string;
    readonly replicas: (count: number) => string;
    readonly signature: Readonly<Record<CheckStatus, string>>;
    readonly sbom: Readonly<Record<CheckStatus, string>>;
    readonly noImage: string;
    readonly copyDigest: (component: ComponentName, version: string | null)
    => string;
  };
  readonly copied: string;
  readonly copyFailed: string;
  readonly yourself: {
    readonly heading: string;
    readonly intro: string;
    readonly selector: string;
    readonly readDigests: string;
    readonly verifyBuild: string;
    readonly sbomNote: readonly [string, string];
    readonly copyCommand: (step: number) => string;
    readonly jsonDocument: string;
    readonly transparencyLog: string;
    readonly buildRun: string;
    readonly cliManual: string;
  };
  readonly limits: {
    readonly heading: string;
    readonly provesLabel: string;
    readonly proves: readonly string[];
    readonly notLabel: string;
    readonly not: readonly string[];
    readonly fullList: string;
  };
}

function plural(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`;
}

export const verifyCopy: Readonly<Record<Locale, VerifyCopy>> = {
  vi: {
    title: 'Kiểm chứng phiên bản đang chạy',
    description: 'Trang này cho thấy chính xác phiên bản đang chạy trên '
      + 'aboutme.vn và cách tự kiểm chứng.',
    lead: 'Trang này cho thấy chính xác phiên bản đang chạy trên aboutme.vn '
      + 'và cách tự kiểm chứng.',
    statusHeading: 'Trạng thái',
    loading: 'Đang tải…',
    unavailable: {
      title: 'Máy chủ này không công bố thông tin triển khai',
      detail: 'Bạn vẫn có thể mở tài liệu JSON hoặc chạy các lệnh bên dưới.',
    },
    outdated: {
      title: 'Trang đã cũ. Hãy tải lại.',
      detail: 'Máy chủ dùng định dạng mới hơn trang này.',
      reload: 'Tải lại',
    },
    stale: {
      title: (time) => `Chưa kiểm tra lại từ ${time}`,
      detail: (age) => `Bản ghi đã cũ ${age}. Các giá trị bên dưới là của `
        + 'lần kiểm tra đó, không phải hiện tại.',
      pill: (version) => `${version} lần cuối`,
    },
    mismatch: {
      title: 'Có thành phần không khớp với bản dựng đã ký',
      detail: {
        not_found: (component) =>
          `${component} đang chạy một image không có chữ ký của GitHub.`,
        invalid: (component) => `Chữ ký của ${component} không hợp lệ.`,
        missing: (component) => `${component} không có image nào đang chạy.`,
      },
      summary: 'Bản tóm tắt không khớp với từng thành phần.',
      more: (count) => `và ${count} thành phần khác`,
    },
    unverified: {
      title: 'Chưa kiểm tra được chữ ký',
      detail: (component) => `Chưa kiểm tra xong chữ ký của ${component}. `
        + 'Trang tự làm mới sau một phút.',
    },
    rollingOut: {
      title: 'Đang cập nhật, hai phiên bản cùng chạy',
      detail: (component, from, to) => `${component} đang chuyển từ ${from} `
        + `sang ${to}. Cả hai bản đều đã được ký.`,
    },
    verified: {
      title: 'Đang chạy đúng mã nguồn trên GitHub',
      detail: (count, version, commit) => `Cả ${count} thành phần chạy bản `
        + `${version}, do GitHub dựng và ký từ commit ${commit}.`,
      detailMixed: 'Mỗi thành phần chạy một bản do GitHub dựng và ký.',
    },
    checkedAt: (time) => `Kiểm tra lúc ${time}`,
    lastCheckedAt: (time) => `Kiểm tra lần cuối lúc ${time}`,
    refreshes: 'Tự làm mới mỗi phút',
    ago: (span) => `${span} trước`,
    justNow: 'vừa xong',
    span: (seconds) => {
      if (seconds >= 86_400) return `${Math.floor(seconds / 86_400)} ngày`;
      if (seconds >= 3600) return `${Math.floor(seconds / 3600)} giờ`;
      return `${Math.max(1, Math.floor(seconds / 60))} phút`;
    },
    chain: {
      heading: 'Từ mã nguồn đến bản đang chạy',
      hint: 'Mỗi bước phải khớp với bước trước.',
      steps: {
        source: 'Mã nguồn',
        build: 'Bản dựng',
        image: 'Image',
        running: 'Đang chạy',
      },
      buildValue: 'GitHub Actions',
      buildLine: (runId) => `release-images, lần chạy ${runId}`,
      imageLine: (count) => `${count} image, đã được GitHub ký`,
      runningLine: (place, replicas, time) =>
        `${place}, ${replicas} bản sao, từ ${time}`,
      chips: {
        match: 'Khớp',
        updating: 'Đang cập nhật',
        not_rechecked: 'Chưa kiểm tra lại',
        not_verified: 'Chưa kiểm chứng',
      },
      failed: {
        not_found: (component) => `${component} không có chữ ký`,
        invalid: () => 'Chữ ký sai',
        missing: (component) => `${component} không chạy`,
      },
      connectors: {
        agree: 'Khớp với bước trước',
        updating: 'Đang cập nhật',
        disagree: 'Không khớp với bước trước',
        unknown: '',
      },
    },
    components: {
      heading: 'Thành phần',
      rollingHint: 'Image mới nhất ở cuối',
      roles: {
        server: 'Máy chủ API',
        web: 'Ứng dụng web',
        caddy: 'Cổng HTTPS',
        maintenance: 'Trang bảo trì',
      },
      versionUnknown: 'Chưa rõ phiên bản',
      older: 'Cũ hơn',
      newest: 'Mới nhất',
      replicas: (count) => `${count} bản sao`,
      signature: {
        verified: 'Chữ ký',
        not_found: 'Không có chữ ký',
        invalid: 'Chữ ký sai',
        unchecked: 'Chưa kiểm tra chữ ký',
        unknown: 'Chưa kiểm tra chữ ký',
      },
      sbom: {
        verified: 'SBOM',
        not_found: 'Không có SBOM',
        invalid: 'SBOM sai',
        unchecked: 'Chưa kiểm tra SBOM',
        unknown: 'Chưa kiểm tra SBOM',
      },
      noImage: 'Không có image nào đang chạy',
      copyDigest: (component, version) =>
        `Sao chép mã băm của ${component}${version ? ` ${version}` : ''}`,
    },
    copied: 'Đã sao chép',
    copyFailed: 'Không sao chép được. Hãy tự chọn và sao chép.',
    yourself: {
      heading: 'Tự kiểm chứng',
      intro: 'Các lệnh này kiểm tra trực tiếp với GitHub và Sigstore, không '
        + 'cần tin aboutme.vn.',
      selector: 'Thành phần',
      readDigests: 'Đọc các mã băm đang chạy',
      verifyBuild: 'Kiểm chứng cách image được dựng (GitHub CLI cần đăng nhập)',
      sbomNote: ['Để kiểm tra SBOM, chạy cùng lệnh với ', '.'],
      copyCommand: (step) => `Sao chép lệnh ${step}`,
      jsonDocument: 'Tài liệu JSON',
      transparencyLog: 'Mục nhật ký minh bạch',
      buildRun: 'Lần chạy bản dựng',
      cliManual: 'Hướng dẫn GitHub CLI',
    },
    limits: {
      heading: 'Trang này chứng minh gì',
      provesLabel: 'Chứng minh',
      proves: [
        'AWS báo cáo đúng các mã băm này đang chạy, với số bản sao như trên.',
        'GitHub đã dựng và ký từng image từ commit công khai được nêu, và chữ '
        + 'ký nằm trong nhật ký công khai của Sigstore.',
      ],
      notLabel: 'Không chứng minh',
      not: [
        'Trang cho thấy những gì AWS báo cáo đang chạy, không chứng minh bản '
        + 'thân máy chủ trung thực.',
        'Trang nói về mã nguồn, không nói về cấu hình hay bí mật.',
        'Bản dựng đã kiểm chứng nghĩa là GitHub dựng nó từ mã nguồn công khai, '
        + 'không có nghĩa mã nguồn không có lỗi.',
      ],
      fullList: 'Danh sách giới hạn đầy đủ trên GitHub',
    },
  },
  en: {
    title: 'Verify what\'s running',
    description: 'This page shows exactly which build runs aboutme.vn and how '
      + 'to check it yourself.',
    lead: 'This page shows exactly which build runs aboutme.vn and how to '
      + 'check it yourself.',
    statusHeading: 'Status',
    loading: 'Loading…',
    unavailable: {
      title: 'This server publishes no deployment record',
      detail: 'You can still open the JSON document or run the commands '
        + 'below.',
    },
    outdated: {
      title: 'This page is out of date. Reload it.',
      detail: 'The server uses a newer format than this page.',
      reload: 'Reload',
    },
    stale: {
      title: (time) => `Not checked since ${time}`,
      detail: (age) => `The record is ${age} old. The values below are from `
        + 'that check, not from now.',
      pill: (version) => `${version} last known`,
    },
    mismatch: {
      title: 'Something running doesn\'t match a signed build',
      detail: {
        not_found: (component) =>
          `${component} runs an image that has no signature from GitHub.`,
        invalid: (component) => `The signature for ${component} is invalid.`,
        missing: (component) => `${component} has no running image.`,
      },
      summary: 'The summary doesn\'t match the components.',
      more: (count) => `and ${count} more`,
    },
    unverified: {
      title: 'Signatures not checked yet',
      detail: (component) => `The signature check for ${component} hasn't `
        + 'finished. The page refreshes in a minute.',
    },
    rollingOut: {
      title: 'Update in progress, two versions running',
      detail: (component, from, to) => `${component} is moving from ${from} `
        + `to ${to}. Both builds are signed.`,
    },
    verified: {
      title: 'Running exactly what\'s on GitHub',
      detail: (count, version, commit) => `All ${count} components run `
        + `${version}, built and signed by GitHub from commit ${commit}.`,
      detailMixed: 'Each component runs a build GitHub built and signed.',
    },
    checkedAt: (time) => `Checked at ${time}`,
    lastCheckedAt: (time) => `Last checked at ${time}`,
    refreshes: 'Refreshes every minute',
    ago: (span) => `${span} ago`,
    justNow: 'just now',
    span: (seconds) => {
      if (seconds >= 86_400) {
        return plural(Math.floor(seconds / 86_400), 'day', 'days');
      }
      if (seconds >= 3600) {
        return plural(Math.floor(seconds / 3600), 'hour', 'hours');
      }
      return plural(Math.max(1, Math.floor(seconds / 60)), 'minute', 'minutes');
    },
    chain: {
      heading: 'From source to running',
      hint: 'Each step must match the one before it.',
      steps: {
        source: 'Source',
        build: 'Build',
        image: 'Image',
        running: 'Running',
      },
      buildValue: 'GitHub Actions',
      buildLine: (runId) => `release-images, run ${runId}`,
      imageLine: (count) =>
        `${plural(count, 'image', 'images')}, signed by GitHub`,
      runningLine: (place, replicas, time) =>
        `${place}, ${plural(replicas, 'replica', 'replicas')}, since ${time}`,
      chips: {
        match: 'Matches',
        updating: 'Updating',
        not_rechecked: 'Not rechecked',
        not_verified: 'Not verified',
      },
      failed: {
        not_found: (component) => `No signature for ${component}`,
        invalid: () => 'Invalid signature',
        missing: (component) => `${component} isn't running`,
      },
      connectors: {
        agree: 'Agrees with the previous step',
        updating: 'Updating',
        disagree: 'Doesn\'t match the previous step',
        unknown: '',
      },
    },
    components: {
      heading: 'Components',
      rollingHint: 'Newest image last',
      roles: {
        server: 'API server',
        web: 'Web app',
        caddy: 'HTTPS proxy',
        maintenance: 'Maintenance page',
      },
      versionUnknown: 'Version unknown',
      older: 'Older',
      newest: 'Newest',
      replicas: (count) => plural(count, 'replica', 'replicas'),
      signature: {
        verified: 'Signature',
        not_found: 'No signature',
        invalid: 'Invalid signature',
        unchecked: 'Signature not checked',
        unknown: 'Signature not checked',
      },
      sbom: {
        verified: 'SBOM',
        not_found: 'No SBOM',
        invalid: 'Invalid SBOM',
        unchecked: 'SBOM not checked',
        unknown: 'SBOM not checked',
      },
      noImage: 'No running image',
      copyDigest: (component, version) =>
        `Copy the ${component} digest${version ? ` ${version}` : ''}`,
    },
    copied: 'Copied',
    copyFailed: 'Couldn\'t copy. Select it and copy it yourself.',
    yourself: {
      heading: 'Verify it yourself',
      intro: 'These commands check against GitHub and Sigstore directly. They '
        + 'don\'t trust aboutme.vn.',
      selector: 'Component',
      readDigests: 'Read the running digests',
      verifyBuild: 'Verify how the image was built (the GitHub CLI must be '
        + 'signed in)',
      sbomNote: ['To check the SBOM, run the same command with ', '.'],
      copyCommand: (step) => `Copy command ${step}`,
      jsonDocument: 'JSON document',
      transparencyLog: 'Transparency log entry',
      buildRun: 'Build run',
      cliManual: 'GitHub CLI manual',
    },
    limits: {
      heading: 'What this proves',
      provesLabel: 'It proves',
      proves: [
        'AWS reports these exact digests as running, with these replica '
        + 'counts.',
        'GitHub built and signed each image from the named public commit, and '
        + 'the signature is in the public Sigstore log.',
      ],
      notLabel: 'It does not prove',
      not: [
        'It shows what AWS reports is running, not proof that the server '
        + 'itself is honest.',
        'It covers the code, not settings or secrets.',
        'A verified build means GitHub built it from public source, not that '
        + 'the source has no bugs.',
      ],
      fullList: 'Full list of limits on GitHub',
    },
  },
};

/** "AWS Singapore" or the GreenNode region, for the Running step. */
export function platformPlace(document: DeploymentDocument): string {
  if (document.provider === 'aws') {
    return document.region === 'ap-southeast-1'
      ? 'AWS Singapore'
      : `AWS ${document.region}`;
  }
  return `GreenNode ${document.region}`;
}

/** 24-hour "10:14" in the viewer's own time zone. */
export function clockTime(at: string | number, locale: Locale): string {
  return new Intl.DateTimeFormat(locale === 'vi' ? 'vi-VN' : 'en-GB', {
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(new Date(at));
}

import type { WorkspaceCopy } from './workspace';

type KindCopy = {
  readonly repository: string;
  readonly pullRequest: string;
  readonly release: string;
};

export type ProjectEvidenceCopy = {
  readonly section: string;
  readonly add: string;
  readonly tabs: KindCopy & { readonly link: string };
  readonly attach: string;
  readonly attached: string;
  readonly labels: KindCopy;
  readonly checked: (date: string) => string;
  readonly checkAgain: string;
  readonly ruleFailed: string;
  readonly gone: string;
  readonly empty: string;
  readonly incomplete: string;
  readonly upstream: string;
  readonly limit: string;
  readonly firstAttach: string;
  readonly remove: string;
  readonly moveUp: (title: string) => string;
  readonly moveDown: (title: string) => string;
  readonly tryAgain: string;
  readonly more: string;
  readonly loading: string;
  readonly sessionEnded: string;
  readonly rateLimit: (time: string) => string;
  readonly chooseRepository: string;
  readonly changeRepository: string;
  readonly pasteLabel: string;
  readonly pasteHint: string;
  readonly notConfirmed: string;
  readonly unsupportedLink: string;
  readonly notFound: string;
  readonly duplicate: string;
  readonly size: string;
};

export const projectEvidenceCopy: WorkspaceCopy<ProjectEvidenceCopy> = {
  vi: {
    section: 'Bằng chứng trên GitHub',
    add: 'Thêm bằng chứng từ GitHub',
    tabs: {
      repository: 'Kho mã',
      pullRequest: 'Pull request',
      release: 'Bản phát hành',
      link: 'Dán đường dẫn',
    },
    attach: 'Gắn',
    attached: 'Đã gắn',
    labels: {
      repository: 'Chủ sở hữu kho mã này',
      pullRequest: 'Tác giả của pull request đã được merge',
      release: 'Tác giả của bản phát hành này',
    },
    checked: (date) => `kiểm tra ngày ${date}`,
    checkAgain: 'Kiểm tra lại',
    ruleFailed: 'Lần kiểm tra gần nhất không còn đáp ứng điều kiện.',
    gone: 'GitHub không còn tìm thấy mục này.',
    empty: 'Không tìm thấy mục nào.',
    incomplete:
      'GitHub chỉ trả về một phần kết quả; hãy dán đường dẫn để tìm thêm.',
    upstream: 'GitHub chưa phản hồi. Hãy thử lại sau.',
    limit: 'Mỗi dự án có tối đa 10 mục.',
    firstAttach:
      'Trang dự án sẽ có đường dẫn tới hoạt động GitHub của bạn, và mọi CV '
      + 'hiện dự án này cũng hiện các mục này.',
    remove: 'Gỡ',
    moveUp: (title) => `Chuyển ${title} lên`,
    moveDown: (title) => `Chuyển ${title} xuống`,
    tryAgain: 'Thử lại',
    more: 'Xem thêm',
    loading: 'Đang tải',
    sessionEnded:
      'Phiên làm việc với GitHub đã kết thúc. Hãy tiếp tục với GitHub để tải '
      + 'lại danh sách.',
    rateLimit: (time) =>
      `Đã chạm giới hạn yêu cầu tới GitHub. Hãy thử lại sau ${time}.`,
    chooseRepository: 'Chọn kho mã',
    changeRepository: 'Đổi kho mã',
    pasteLabel: 'Đường dẫn GitHub',
    pasteHint:
      'Kho mã, pull request hoặc bản phát hành, ví dụ github.com/ten/kho, '
      + 'github.com/ten/kho/pull/12 hoặc github.com/ten/kho/releases/tag/v1.0',
    notConfirmed:
      'GitHub không xác nhận bạn là tác giả hoặc chủ sở hữu của mục này.',
    unsupportedLink:
      'Đường dẫn này không phải kho mã, pull request hay bản phát hành trên '
      + 'GitHub.',
    notFound: 'Mục này không còn trong dự án. Danh sách đã được tải lại.',
    duplicate: 'Mục này đã có trong dự án.',
    size:
      'Dự án đã đạt dung lượng tối đa. Hãy rút gọn nội dung hoặc gỡ bớt mục.',
  },
  en: {
    section: 'GitHub evidence',
    add: 'Add evidence from GitHub',
    tabs: {
      repository: 'Repositories',
      pullRequest: 'Pull requests',
      release: 'Releases',
      link: 'Paste a link',
    },
    attach: 'Attach',
    attached: 'Attached',
    labels: {
      repository: 'Owner of this repository',
      pullRequest: 'Author of a merged pull request',
      release: 'Author of this release',
    },
    checked: (date) => `checked on ${date}`,
    checkAgain: 'Check again',
    ruleFailed: 'The last check no longer met the rule.',
    gone: 'GitHub no longer finds this item.',
    empty: 'Nothing found.',
    incomplete:
      'GitHub returned only part of the results; paste a link to find more.',
    upstream: 'GitHub did not respond. Try again later.',
    limit: 'A project holds at most 10 items.',
    firstAttach:
      'The project page will link to your GitHub activity, and every resume '
      + 'that shows this project shows these items.',
    remove: 'Remove',
    moveUp: (title) => `Move ${title} up`,
    moveDown: (title) => `Move ${title} down`,
    tryAgain: 'Try again',
    more: 'Show more',
    loading: 'Loading',
    sessionEnded:
      'Your GitHub session has ended. Continue with GitHub to load the list '
      + 'again.',
    rateLimit: (time) =>
      `The GitHub request limit is reached. Try again after ${time}.`,
    chooseRepository: 'Choose a repository',
    changeRepository: 'Change repository',
    pasteLabel: 'GitHub link',
    pasteHint:
      'A repository, pull request or release, such as github.com/name/repo, '
      + 'github.com/name/repo/pull/12 or github.com/name/repo/releases/tag/'
      + 'v1.0',
    notConfirmed:
      'GitHub does not confirm you as the author or owner of this item.',
    unsupportedLink:
      'This link is not a GitHub repository, pull request or release.',
    notFound:
      'This item is no longer on the project. The list has been reloaded.',
    duplicate: 'This item is already on the project.',
    size:
      'The project has reached its size limit. Shorten its text or remove an '
      + 'item.',
  },
};

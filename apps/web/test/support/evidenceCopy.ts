export const DESIGN_COPY: readonly (readonly [string, string, string])[]
  = [
    [
      'Section',
      'Bằng chứng trên GitHub',
      'GitHub evidence',
    ],
    [
      'Add',
      'Thêm bằng chứng từ GitHub',
      'Add evidence from GitHub',
    ],
    [
      'Tabs',
      'Kho mã · Pull request · Bản phát hành · Dán đường dẫn',
      'Repositories · Pull requests · Releases · Paste a link',
    ],
    [
      'Attach',
      'Gắn',
      'Attach',
    ],
    [
      'Attached',
      'Đã gắn',
      'Attached',
    ],
    [
      'Pull request label',
      'Tác giả của pull request đã được merge',
      'Author of a merged pull request',
    ],
    [
      'Release label',
      'Tác giả của bản phát hành này',
      'Author of this release',
    ],
    [
      'Repository label',
      'Chủ sở hữu kho mã này',
      'Owner of this repository',
    ],
    [
      'Checked',
      'kiểm tra ngày {date}',
      'checked on {date}',
    ],
    [
      'Check again',
      'Kiểm tra lại',
      'Check again',
    ],
    [
      'Rule failed',
      'Lần kiểm tra gần nhất không còn đáp ứng điều kiện.',
      'The last check no longer met the rule.',
    ],
    [
      'Gone',
      'GitHub không còn tìm thấy mục này.',
      'GitHub no longer finds this item.',
    ],
    [
      'Empty',
      'Không tìm thấy mục nào.',
      'Nothing found.',
    ],
    [
      'Incomplete',
      'GitHub chỉ trả về một phần kết quả; hãy dán đường dẫn để tìm thêm.',
      'GitHub returned only part of the results; paste a link to find '
      + 'more.',
    ],
    [
      'Upstream',
      'GitHub chưa phản hồi. Hãy thử lại sau.',
      'GitHub did not respond. Try again later.',
    ],
    [
      'Limit',
      'Mỗi dự án có tối đa 10 mục.',
      'A project holds at most 10 items.',
    ],
    [
      'First attach',
      'Trang dự án sẽ có đường dẫn tới hoạt động GitHub của bạn, và mọi CV '
      + 'hiện dự án này cũng hiện các mục này.',
      'The project page will link to your GitHub activity, and every '
      + 'resume that shows this project shows these items.',
    ],
    [
      'Different account',
      'Tài khoản GitHub này khác tài khoản đã kết nối. Hãy ngắt kết nối '
      + 'trước.',
      'This GitHub account differs from the connected one. Disconnect '
      + 'first.',
    ],
    [
      'Disconnect ask',
      'Giữ các mục đã gắn dưới dạng đường dẫn thường?',
      'Keep the attached items as plain links?',
    ],
    [
      'Disconnect keep',
      'Giữ đường dẫn',
      'Keep links',
    ],
    [
      'Disconnect remove',
      'Xóa khỏi mọi dự án',
      'Remove from every project',
    ],
    [
      'Remove',
      'Gỡ',
      'Remove',
    ],
    [
      'Move up',
      'Chuyển {title} lên',
      'Move {title} up',
    ],
    [
      'Move down',
      'Chuyển {title} xuống',
      'Move {title} down',
    ],
    [
      'Try again',
      'Thử lại',
      'Try again',
    ],
    [
      'More',
      'Xem thêm',
      'Show more',
    ],
    [
      'Loading',
      'Đang tải',
      'Loading',
    ],
    [
      'Session ended',
      'Phiên làm việc với GitHub đã kết thúc. Hãy tiếp tục với GitHub để '
      + 'tải lại danh sách.',
      'Your GitHub session has ended. Continue with GitHub to load the '
      + 'list again.',
    ],
    [
      'Rate limit',
      'Đã chạm giới hạn yêu cầu tới GitHub. Hãy thử lại sau {time}.',
      'The GitHub request limit is reached. Try again after {time}.',
    ],
    [
      'Choose a repository',
      'Chọn kho mã',
      'Choose a repository',
    ],
    [
      'Change repository',
      'Đổi kho mã',
      'Change repository',
    ],
    [
      'Paste label',
      'Đường dẫn GitHub',
      'GitHub link',
    ],
    [
      'Paste hint',
      'Kho mã, pull request hoặc bản phát hành, ví dụ github.com/ten/kho, '
      + 'github.com/ten/kho/pull/12 hoặc '
      + 'github.com/ten/kho/releases/tag/v1.0',
      'A repository, pull request or release, such as '
      + 'github.com/name/repo, github.com/name/repo/pull/12 or '
      + 'github.com/name/repo/releases/tag/v1.0',
    ],
    [
      'Not confirmed',
      'GitHub không xác nhận bạn là tác giả hoặc chủ sở hữu của mục này.',
      'GitHub does not confirm you as the author or owner of this item.',
    ],
    [
      'Unsupported link',
      'Đường dẫn này không phải kho mã, pull request hay bản phát hành '
      + 'trên GitHub.',
      'This link is not a GitHub repository, pull request or release.',
    ],
    [
      'Not found',
      'Mục này không còn trong dự án. Danh sách đã được tải lại.',
      'This item is no longer on the project. The list has been reloaded.',
    ],
    [
      'Duplicate',
      'Mục này đã có trong dự án.',
      'This item is already on the project.',
    ],
    [
      'Size',
      'Dự án đã đạt dung lượng tối đa. Hãy rút gọn nội dung hoặc gỡ bớt '
      + 'mục.',
      'The project has reached its size limit. Shorten its text or remove '
      + 'an item.',
    ],
    [
      'Connection changed',
      'Kết nối GitHub vừa thay đổi. Trạng thái đã được tải lại; hãy thử '
      + 'lại.',
      'The GitHub connection just changed. The status has been reloaded; '
      + 'try again.',
    ],
  ];

export function designCopy(): Map<string, readonly [string, string]> {
  return new Map(DESIGN_COPY.map(([key, vi, en]) => [key, [vi, en]]));
}

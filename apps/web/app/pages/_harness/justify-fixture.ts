import type { Resume } from '@aboutme/schema';

/**
 * Body text long enough to wrap, so the justify screenshot cell shows
 * justified paragraphs and list items (ADR 0041). The vn-full fixture's body
 * lines each fit on one line, where justify has no visible effect.
 */
const PROFILE_TEXT
  = '<p>Kỹ sư giàu kinh nghiệm với hơn mười năm xây dựng hệ thống phân tán '
    + 'cho ngân hàng, thương mại điện tử và dịch vụ công. Tôi tập trung vào độ '
    + 'tin cậy, khả năng quan sát và trải nghiệm của nhà phát triển, đồng thời '
    + 'hướng dẫn đội ngũ áp dụng kiểm thử tự động, triển khai liên tục và đánh '
    + 'giá thiết kế định kỳ để sản phẩm dễ sử dụng và dễ bảo trì.</p>';

const WORK_DESCRIPTION
  = '<ul><li>Dẫn dắt thiết kế nền tảng dữ liệu phục vụ hàng triệu người dùng '
    + 'mỗi ngày, giảm độ trễ truy vấn trung bình xuống còn một phần ba so với '
    + 'hệ thống cũ.</li><li>Hướng dẫn đội ngũ kỹ sư xây dựng quy trình đánh '
    + 'giá mã nguồn, giám sát sản xuất và phản hồi sự cố trong vòng mười lăm '
    + 'phút.</li></ul>';

export function withWrappingBody(document: Resume): void {
  const profile = document.content.profile;
  if (profile?.sectionType === 'profile' && profile.entries[0] !== undefined) {
    profile.entries[0].text = PROFILE_TEXT;
  }
  const work = document.content.work;
  if (work?.sectionType === 'work' && work.entries[0] !== undefined) {
    work.entries[0].description = WORK_DESCRIPTION;
  }
}

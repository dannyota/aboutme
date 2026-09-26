# Legal basis and notices

This page checks viewer analytics against the Personal Data Protection Law
91/2025/QH15 (the Law) and Decree 356/2025/NĐ-CP (the Decree), both in force
since 1 January 2026 (Law Article 38(1); Decree Article 42(1)). Citations were
read on the official gazette texts: the Law in Công báo 971 + 972 of 24 July
2025 ([law]), the Decree in Công báo 18 of 18 January 2026 ([decree]). The
findings are an engineering reading, not legal advice; the privacy and
disclosure review by qualified counsel stays a launch gate
([decisions](../decisions.md#remaining-gates)).

The design keeps no data about any viewer. That choice removes the consent
popup, viewer cookies other than the sign-in pass, the viewer rights page, owner
terms, and the need to record viewers in the DPIA before a release.

## Roles

**aboutme is the controller and processor** (bên kiểm soát và xử lý dữ liệu cá
nhân, Law Article 2(9)) of the transient viewer data that counting and sign in
to view use: it decides the purposes (count real views; keep bots off a
`sign_in` resume) and the means (fields, filters, retention) and processes the
data itself. The resume owner receives only daily totals, which describe no
person, and cannot change what is processed.

- **AWS** processes on aboutme's behalf as today (Law Article 2(8)): CloudFront
  and AWS WAF see the request, including the IP address, to label bots.
- **Google and LinkedIn** verify the viewer in sign in to view, as independent
  controllers, as they do for account sign-in.

The owner must not be the controller with aboutme as processor. A service that
operates automated systems to process personal data on behalf of controllers, or
that collects personal data online from websites, is a personal data processing
service (Decree Article 21(1) and (3)). Only an organization or enterprise may
run one (Decree Article 22(1)), under a certificate from the Ministry of Public
Security (Decree Article 24(1)). aboutme is run by an individual. Keeping the
processing fixed by aboutme and giving the owner only totals avoids that role.
This is the reason for
[ADR 0060](../../adr/0060-viewer-data-controller-and-consent.md).

## What the data is

- **Personal data:** data that identifies or helps identify a specific person;
  data after de-identification is no longer personal data (Law Article 2(1)).
  Daily counts and share signals describe no person, so they are not personal
  data. The IP address and user agent are personal data while they are
  processed, and so is the in-memory network key for the day it exists.
- **Sensitive:** "data tracking the behavior and activity of using ... services
  in cyberspace" is sensitive personal data (Decree Article 4(1)(l)). Nothing
  here records a person's activity: counts are totals, and sign in to view keeps
  no identity, so no sensitive personal data is stored.
- **The pass cookie** holds a signed value that names a resume and an expiry,
  not a person, and aboutme keeps no copy linking it to anyone
  ([sign in to view](sign-in-to-view.md#pass-cookie)).

## Lawful basis

| Processing                                | Basis                                                                                                                                                                                                                                                                                                                                                                                                       |
| ----------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Anonymous counts                          | The stored result is not personal data. The IP address and user agent are processed in memory to serve the page, apply rate limits, and form the day's network key, then dropped. **Unverified:** no consent-free case in Law Article 19(1) names this; counsel confirms that transient processing whose only output is a count needs no consent. The notice discloses it either way (Law Article 4(1)(a)). |
| Sign-in verification in `sign_in` resumes | Consent, given on the gate before the viewer continues (Law Article 11(1)); the name and email the provider returns are discarded in the same request                                                                                                                                                                                                                                                       |
| Pass cookie                               | Needed to give the viewer the access they asked for; the gate text says it is set                                                                                                                                                                                                                                                                                                                           |

## Sign-in gate text

Shown on the gate of a `sign_in` resume (**Owner approval** V4; the owner
reviews the Vietnamese). Both texts name only the providers enabled when the
gate renders.

| Language   | Text                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Vietnamese | **Đăng nhập để xem CV này.** Chủ CV yêu cầu người xem đăng nhập để chặn bot và việc sao chép tự động. Khi bạn tiếp tục, Google hoặc LinkedIn xác nhận tài khoản của bạn với aboutme. aboutme không lưu tên hay email của bạn và không cho chủ CV biết bạn là ai; chúng tôi chỉ đặt một cookie trên trình duyệt này để bạn xem CV này trong 7 ngày. Việc đăng nhập không tạo tài khoản aboutme. Chỉ tiếp tục nếu bạn từ 16 tuổi trở lên.   |
| English    | **Sign in to view this resume.** The owner asks viewers to sign in, to keep out bots and automated copying. If you continue, Google or LinkedIn confirms your account to aboutme. aboutme does not keep your name or email and does not tell the owner who you are; it only sets a cookie in this browser that lets you view this resume for 7 days. Signing in does not create an aboutme account. Continue only if you are 16 or older. |

**Unverified here:** that "child" means under 16 comes from the Law on Children
2016, Article 1, not re-read for this page (Law Article 24(2)).

## Retention

Law Article 3(3) keeps data only as long as its purpose needs; Law Article
14(1)(b) deletes it when the purpose ends.

| Data                              | Kept                                                |
| --------------------------------- | --------------------------------------------------- |
| IP address and user agent         | In memory for the request                           |
| Network key and day key           | In memory until the end of the Asia/Ho_Chi_Minh day |
| Daily counts and share signals    | 400 days, then the privacy sweep deletes them       |
| Provider name and email (sign-in) | Not kept: discarded in the callback request         |
| Pass cookie (sign-in)             | 7 days, in the viewer's browser only                |

## Assessments

A data protection impact assessment covers aboutme's processing and is updated
when a new processing purpose arises (Law Articles 21(1) and 22(1); Decree
Articles 19 and 20(1)(a)). Viewer analytics stores no viewer data, so no release
waits on an assessment; the owner adds the transient counting and sign-in
verification to the next regular update of the existing DPIA and cross-border
assessment (Law Article 20(2); Decree Article 18). The dossiers stay out of this
public repository.

## Privacy notice changes

`apps/web/app/i18n/legal.ts`, both languages, in the release that first needs
each item (**Owner approval** V4; the owner reviews the Vietnamese). The notice
never implies tracking.

| Release         | Section          | Change (English; the Vietnamese mirrors it)                                                                                                                                                                                                                          |
| --------------- | ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| View counts     | What we collect  | New item: "Views of public resumes: we count views and keep only daily totals. To tell people from bots, your IP address and browser details are used in memory only and discarded the same day, and your browser solves a small computing task. No cookie is used." |
| View counts     | What we don't do | Replace "No analytics or tracking scripts." with "No third-party analytics or advertising trackers. We count views of public resumes only as described above."                                                                                                       |
| Sign in to view | What we collect  | New item: "If a resume owner requires sign-in to view: Google or LinkedIn confirms your account, and we discard your name and email at once. The owner is not told who you are."                                                                                     |
| Sign in to view | Cookies          | Add the pass cookie, which lets you view that resume for 7 days, and the join-invite `localStorage` entry.                                                                                                                                                           |

Vietnamese for the view-counts rows: "Lượt xem CV công khai: chúng tôi đếm lượt
xem và chỉ lưu tổng số theo ngày. Để phân biệt người với bot, địa chỉ IP và
thông tin trình duyệt của bạn chỉ được dùng trong bộ nhớ và bị xoá trong ngày,
và trình duyệt của bạn giải một phép tính nhỏ. Không dùng cookie." and "Không
dùng công cụ phân tích hay mã theo dõi của bên thứ ba, không dùng mã quảng cáo.
Chúng tôi chỉ đếm lượt xem CV công khai như mô tả ở trên."

## Articles that do not change the design

- Law Article 29(3) and (4) (cookie refusal and "do not track" for social
  networks and online communication services): aboutme is likely neither, and
  counting sets no cookie.
- Law Article 28 (advertising): no advertising.
- Law Article 25 (recruitment): governs employers' handling of candidates' data,
  not the viewers of a candidate's resume.

## Not verified

- The banhmi legal search tool was not available when these citations were read;
  every citation above was read directly on the gazette PDFs linked below.
- Whether transient in-memory processing for counts needs consent (above).
- The age of a child (Law on Children 2016), and the administrative sanctions
  decree for data protection; neither was read for this page.

[law]:
  https://congbaocdn.chinhphu.vn/CongBaoCP/VanBan/2025/6/45578/57730-1-2025971-97291-2025-qh15.pdf
[decree]:
  https://congbaocdn.chinhphu.vn/180507251028987904/2026/1/17/356signed-1768638052103952849513.pdf

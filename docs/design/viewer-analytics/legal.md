# Legal basis and notices

This page checks viewer analytics against the Personal Data Protection Law
91/2025/QH15 (the Law) and Decree 356/2025/NĐ-CP (the Decree), both in force
since 1 January 2026 (Law Article 38(1); Decree Article 42(1)). Citations were
read on the official gazette texts: the Law in Công báo 971 + 972 of 24 July
2025 ([law]), the Decree in Công báo 18 of 18 January 2026 ([decree]). The
findings are an engineering reading, not legal advice; the privacy and
disclosure review by qualified counsel stays a launch gate
([decisions](../decisions.md#remaining-gates)).

## Roles

**aboutme is the controller and processor** (bên kiểm soát và xử lý dữ liệu cá
nhân, Law Article 2(9)) of all viewer data: it decides the purposes (show a
resume owner how their resume is read) and the means (fields, filters,
retention) and processes the data itself. The resume owner only picks a mode.

- **The resume owner** receives data about viewers who agreed. That is provision
  of personal data to another individual with the data subject's consent (Law
  Article 15(2)(b)). The owner is responsible for any copy they take off
  aboutme. The owner terms below also cover the contents a transfer agreement
  needs (Decree Article 7(1)), in case counsel classes the flow as a transfer
  under Law Article 17(1)(a).
- **AWS** processes the data on aboutme's behalf as today (Law Article 2(8)):
  CloudFront derives city and country from the IP address; the database and
  backups are in Singapore.
- **Google and LinkedIn** verify the viewer in sign in to view, as independent
  controllers, as they do for account sign-in.

The owner must not be the controller with aboutme as processor. A service that
operates automated systems to process personal data on behalf of controllers, or
that collects personal data online from websites, is a personal data processing
service (Decree Article 21(1) and (3)). Only an organization or enterprise may
run one (Decree Article 22(1)), under a certificate from the Ministry of Public
Security (Decree Article 24(1)). aboutme is run by an individual. The design
keeps the fields, purposes, and retention fixed by aboutme for every resume, and
gives the owner no way to change them. This is the reason for
[ADR 0060](../../adr/0060-viewer-data-controller-and-consent.md).

## What the data is

- **Personal data:** data that identifies or helps identify a specific person;
  data after de-identification is no longer personal data (Law Article 2(1)).
  Daily counts and share signals describe no person, so they are not personal
  data. The in-memory network key is personal data for the day it exists.
- **Sensitive:** "data tracking the behavior and activity of using
  telecommunication services, social networks, online communication services,
  and other services in cyberspace" is sensitive personal data (Decree Article
  4(1)(l)). Recorded views of a named or cookie-keyed viewer are such data. The
  design treats view events, viewer identities, and consent records as
  sensitive. Consequences: the consent request must say the data is sensitive
  (Decree Article 6(4)); access is limited and the processing written down
  (Decree Article 4(2)).
- **Location:** city and country from an IP address are not "location determined
  through a positioning service" (Decree Article 4(1)(h)); aboutme uses no
  positioning service.

## Lawful basis

| Processing                                 | Basis                                                                                                                                                                                                                                                                                                                                                                                                       |
| ------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Anonymous counts                           | The stored result is not personal data. The IP address and user agent are processed in memory to serve the page, apply rate limits, and form the day's network key, then dropped. **Unverified:** no consent-free case in Law Article 19(1) names this; counsel confirms that transient processing whose only output is a count needs no consent. The notice discloses it either way (Law Article 4(1)(a)). |
| View events in `ask` mode                  | Consent, collected before collection (Law Article 11(1))                                                                                                                                                                                                                                                                                                                                                    |
| Identity and view events in `sign_in` mode | Consent, given on the gate before sign-in                                                                                                                                                                                                                                                                                                                                                                   |
| Consent records                            | Required to store consent and prove it (Decree Article 6(2))                                                                                                                                                                                                                                                                                                                                                |

## Consent

The popup and the gate meet each requirement:

| Requirement                                                                      | Where met                                                                                                                                                                       |
| -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Voluntary and informed of data types, purpose, controller, and rights (Law 9(2)) | The text names every field, the purpose, aboutme and its operator, and the rights                                                                                               |
| Clear, specific, and recordable in electronic form (Law 9(3); Decree 6(1)(d))    | A button press on the site; the notice version is stored with the record                                                                                                        |
| One purpose per consent, no bundling (Law 9(4)(a), (b))                          | Consent covers one purpose for one resume; reading the resume never depends on it                                                                                               |
| Silence is not consent (Law 9(4)(d))                                             | Ignoring or closing the popup records nothing                                                                                                                                   |
| No default consent, no confusing choice (Decree 6(3))                            | Two equal buttons, nothing preselected                                                                                                                                          |
| Stored consent, controller proves it (Decree 6(2))                               | `view_consents` rows, kept 180 days                                                                                                                                             |
| Told the data is sensitive (Decree 6(4))                                         | One sentence in both texts                                                                                                                                                      |
| Children's data through a legal representative (Law 24(2))                       | Both texts ask only people aged 16 or over to agree. **Unverified here:** that "child" means under 16 comes from the Law on Children 2016, Article 1, not re-read for this page |
| Withdrawal, not retroactive (Law 10(1), 10(4))                                   | The popup links to `/privacy/viewer`; withdrawal also deletes, which Law 14(1)(a) allows on request                                                                             |

### Consent popup text

| Part          | Vietnamese                                                                                                                                                                                                                                                                                                                                                                                       |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Title         | Chủ CV muốn biết CV được xem thế nào                                                                                                                                                                                                                                                                                                                                                             |
| Body          | Nếu bạn đồng ý, aboutme đặt một cookie ngẫu nhiên trên trình duyệt này và ghi lại mỗi lần bạn xem CV này: thời điểm, thời gian xem, loại thiết bị, thành phố hoặc quốc gia (suy ra từ địa chỉ IP; không lưu địa chỉ IP) và nguồn truy cập (như Zalo, LinkedIn, email). Chủ CV sẽ thấy các thông tin này và biết các lần xem đến từ cùng một trình duyệt, nhưng không thấy tên hay email của bạn. |
| Sensitive     | Đây là dữ liệu theo dõi hoạt động trên không gian mạng, thuộc dữ liệu cá nhân nhạy cảm theo pháp luật Việt Nam.                                                                                                                                                                                                                                                                                  |
| Controller    | Bên kiểm soát dữ liệu: aboutme, do Danny vận hành. Dữ liệu được lưu tại Singapore và xoá sau 90 ngày.                                                                                                                                                                                                                                                                                            |
| Choice        | Bạn có thể xem, xoá hoặc rút lại đồng ý bất cứ lúc nào. Nếu không đồng ý, bạn vẫn xem CV bình thường; chúng tôi chỉ đếm một lượt xem ẩn danh. Chỉ đồng ý nếu bạn từ 16 tuổi trở lên.                                                                                                                                                                                                             |
| Buttons, link | Đồng ý · Không đồng ý · Chi tiết và rút lại đồng ý                                                                                                                                                                                                                                                                                                                                               |

| Part          | English                                                                                                                                                                                                                                                                                                                                                                           |
| ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Title         | The owner would like to know how this resume is read                                                                                                                                                                                                                                                                                                                              |
| Body          | If you agree, aboutme sets a random cookie in this browser and records each time you view this resume: the time, how long you view it, your device type, your city or country (from your IP address, which is not stored), and where you came from (such as Zalo, LinkedIn, or email). The owner sees this and that the visits come from one browser, but not your name or email. |
| Sensitive     | This is data tracking your activity online, which Vietnamese law treats as sensitive personal data.                                                                                                                                                                                                                                                                               |
| Controller    | Controller: aboutme, operated by Danny. Stored in Singapore and deleted after 90 days.                                                                                                                                                                                                                                                                                            |
| Choice        | You can see, delete, or withdraw at any time. If you decline, you still read the resume; we only count an anonymous view. Agree only if you are 16 or older.                                                                                                                                                                                                                      |
| Buttons, link | Agree · Decline · Details and withdrawal                                                                                                                                                                                                                                                                                                                                          |

### Sign-in gate text

| Language   | Text                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Vietnamese | **Đăng nhập để xem CV này.** Chủ CV yêu cầu người xem đăng nhập. Khi bạn tiếp tục, Google hoặc LinkedIn gửi cho aboutme tên và email của bạn. Chủ CV sẽ thấy tên, email của bạn và mỗi lần bạn xem CV này: thời điểm, thời gian xem, loại thiết bị, thành phố hoặc quốc gia và nguồn truy cập. Đây là dữ liệu theo dõi hoạt động trên không gian mạng, thuộc dữ liệu cá nhân nhạy cảm. Việc đăng nhập không tạo tài khoản aboutme. Bên kiểm soát dữ liệu: aboutme, do Danny vận hành; dữ liệu lưu tại Singapore và xoá 90 ngày sau lần xem cuối. Bạn có thể xem, xoá hoặc rút lại đồng ý bất cứ lúc nào. Chỉ tiếp tục nếu bạn từ 16 tuổi trở lên.      |
| English    | **Sign in to view this resume.** The owner asks viewers to sign in. If you continue, Google or LinkedIn sends aboutme your name and email. The owner sees your name, your email, and each time you view this resume: the time, how long, your device type, your city or country, and where you came from. This is data tracking your activity online, which Vietnamese law treats as sensitive personal data. Signing in does not create an aboutme account. Controller: aboutme, operated by Danny; stored in Singapore and deleted 90 days after your last view. You can see, delete, or withdraw at any time. Continue only if you are 16 or older. |

Both texts name only the providers enabled when the gate renders.

## Viewer rights

| Right (Law Article 4(1))                | How a viewer uses it                                                                    | Decree Article 5 limit                                          |
| --------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| To know (a)                             | Popup, gate, privacy notice                                                             | Not applicable                                                  |
| To consent, refuse, and withdraw (b)    | Popup buttons; `/privacy/viewer`                                                        | Respond in 2 working days, stop in 15 days (5(2)); done at once |
| To view and have data provided (c), (d) | `/privacy/viewer` lists every record for the cookie or pass                             | 2 working days, 10 days (5(3)); done at once                    |
| To correct (c)                          | Not applicable: fields are measured; a viewer who signs in again updates name and email | 10 days (5(3))                                                  |
| To delete, restrict, and object (d)     | Withdraw and delete on `/privacy/viewer`; email for anything else                       | 2 working days, 20 days (5(4)); done at once                    |
| To complain and claim damages (đ)       | Privacy notice                                                                          | Not applicable                                                  |

The Decree requires a written procedure for these rights (Decree Article 5(1));
`/privacy/viewer` and the privacy notice are that procedure. A viewer whose
cookie is gone cannot be matched; the notice says so.

## Retention

Law Article 3(3) keeps data only as long as its purpose needs; Law Article
14(1)(b) deletes it when the purpose ends. The periods are in the
[overview](README.md#rules); the privacy sweep enforces them daily. The owner
terms state 90 days so the owner's own copies follow the same rule.

## DPIA

A data protection impact assessment is required. The controller prepares and
keeps a DPIA dossier and sends one original to the data protection authority
within 60 days of first processing (Law Article 21(1); Decree Article 19, Form
10). It is updated every 6 months when a new processing purpose arises (Law
Article 22(1); Decree Article 20(1)(a)). The exemptions in Law Article 38(2) and
(3) and Decree Article 41 cover small, start-up, household, and micro businesses
only, and even they lose them when they process sensitive data; aboutme is run
by an individual and viewer tracking is sensitive. Viewer data is stored in
Singapore, which is a cross-border transfer (Law Article 20(1); Decree Article
17(1)(a)), so the cross-border assessment (Law Article 20(2); Decree Article 18,
Form 09) must cover it too. **Owner approval** V7: both dossiers cover viewer
data before viewer tracking ships. The dossiers stay out of this public
repository.

## Terms for owners

Shown when an owner picks `ask` or `sign_in`, and added to the Terms of Service:

- vi: "Khi bật theo dõi người xem hoặc yêu cầu đăng nhập, bạn nhận dữ liệu cá
  nhân của người xem. Chỉ dùng dữ liệu đó để theo dõi việc ứng tuyển của bạn;
  không công khai, không bán, không chia sẻ cho người khác. aboutme xoá dữ liệu
  sau 90 ngày; bạn xoá mọi bản sao bạn lưu trong cùng thời hạn."
- en: "If you turn on viewer tracking or sign-in to view, you receive viewers'
  personal data. Use it only to follow up on your job search; do not publish,
  sell, or share it. aboutme deletes it after 90 days; delete any copy you keep
  within the same time."

## Privacy notice changes

`apps/web/app/i18n/legal.ts`, both languages, in the release that first needs
each item (**Owner approval** V4; the owner reviews the Vietnamese):

| Section                   | Change (English; the Vietnamese mirrors it)                                                                                                                                                                                                                          |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| What we collect (counts)  | New item: "Views of public resumes: we count views and keep only daily totals. To tell people from bots, your IP address and browser details are used in memory only and discarded the same day, and your browser solves a small computing task. No cookie is used." |
| What we collect (viewers) | New item: "If a resume owner asks and you agree, or you sign in to view a resume: the data in the notice you were shown, kept 90 days, and a record of your choice, kept 180 days. This is sensitive personal data. Your consent choice is stored in a cookie."      |
| What we do not do         | Replace "No analytics or tracking code." with "No third-party analytics or advertising trackers. We track views only as described above." Add the three viewer cookies and the join-invite `localStorage` entry to the cookie and storage items.                     |
| Purpose and basis         | Add: "For people viewing a resume: we count views without consent because we keep no personal data from it; we record details only with your consent, which you can withdraw."                                                                                       |
| Your rights               | Add: "Viewers can see, withdraw, and delete their data at /privacy/viewer. We can only find it through the cookie or sign-in on your browser; otherwise email us."                                                                                                   |
| Where stored              | No change: Singapore, CloudFront, Google; add LinkedIn only if LinkedIn sign-in has not already added it.                                                                                                                                                            |

Vietnamese for the first two rows: "Lượt xem CV công khai: chúng tôi đếm lượt
xem và chỉ lưu tổng số theo ngày. Để phân biệt người với bot, địa chỉ IP và
thông tin trình duyệt của bạn chỉ được dùng trong bộ nhớ và bị xoá trong ngày,
và trình duyệt của bạn giải một phép tính nhỏ. Không dùng cookie." and "Nếu chủ
CV hỏi và bạn đồng ý, hoặc bạn đăng nhập để xem một CV: các dữ liệu nêu trong
thông báo bạn đã thấy, lưu 90 ngày, và bản ghi lựa chọn của bạn, lưu 180 ngày.
Đây là dữ liệu cá nhân nhạy cảm. Lựa chọn đồng ý của bạn được lưu trong cookie."

## Articles that do not change the design

- Law Article 29(3) and (4) (cookie refusal and "do not track" for social
  networks and online communication services): aboutme is likely neither, and
  the design meets both anyway.
- Law Article 28 (advertising): no advertising.
- Law Article 25 (recruitment): governs employers' handling of candidates' data,
  not the viewers of a candidate's resume.

## Not verified

- The banhmi legal search tool was not available in this session; every citation
  above was read directly on the gazette PDFs linked below.
- Whether an individual running a free service is held to the DPIA filing duty
  as an enterprise is; the Law applies to Vietnamese individuals (Law Article
  1(2)(a)) and exempts only named businesses, so the design assumes it is.
- Whether transient in-memory processing for counts needs consent (above).
- The age of a child (Law on Children 2016), and the administrative sanctions
  decree for data protection; neither was read for this page.

[law]:
  https://congbaocdn.chinhphu.vn/CongBaoCP/VanBan/2025/6/45578/57730-1-2025971-97291-2025-qh15.pdf
[decree]:
  https://congbaocdn.chinhphu.vn/180507251028987904/2026/1/17/356signed-1768638052103952849513.pdf

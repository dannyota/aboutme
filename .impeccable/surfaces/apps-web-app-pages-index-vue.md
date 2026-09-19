---
version: 1
slug: "apps-web-app-pages-index-vue"
primary_target: "apps/web/app/pages/index.vue"
related_targets:
  [
    "apps/web/app/components/editor/EditorShell.vue",
    "apps/web/app/components/editor/list/ResumeList.vue",
    "apps/web/app/pages/login.vue",
    "apps/web/app/components/editor/PublishDialog.vue",
  ]
---

# Landing surface record

Scope: `/` (Nuxt server-side rendering, compiled-in content, no data fetch).
Visitor mode: Persuade. The same visual world continues through account pages,
the resume list, settings, the editor shell, and the publish dialog.

## Audience and job

A visitor is deciding whether the product is worth an account. Vietnamese is the
default site language; the English toggle serves visitors who prefer English.
The visitor must understand that each resume publishes as a document at its own
link, no account profile becomes public, and discovery stays under the resume
owner's control.

## Proof and content

- The compiled-in Ada Lovelace document renders through `ResumeDocument` on the
  server with no data fetch. Its two-column customization uses Letter page
  metadata and does not apply a gallery preset.
- The three facts are Yours to keep, One link per resume, and Bring your own
  agent. The three publish choices are Public resume, PDF download, and SEO and
  GEO.
- The page contains no testimonials, user counts, or customer logos. The footer
  links the AGPL-3.0 repository, Terms, and Privacy.

## Hero copy

| Site language | Headline                                                     | Lead                                                                                                                                                                                          |
| ------------- | ------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Vietnamese    | “CV của bạn. Miễn phí. Không ai thấy nếu bạn không muốn.”    | “aboutme là công cụ tạo CV mã nguồn mở. Viết tối đa ba CV, xem trước đúng từng trang, và đăng mỗi CV tại một đường dẫn riêng. Tìm kiếm và khám phá bằng AI luôn tắt cho đến khi bạn bật.”     |
| English       | “Your resume. Free. No one sees it unless you want them to.” | “aboutme is an open-source resume builder. Write up to three resumes, preview the exact page, and publish each one at its own link. Search and AI discovery stay off until you turn them on.” |

Signed-out actions appear in this order:

| Site language | Primary        | Secondary        | Text link |
| ------------- | -------------- | ---------------- | --------- |
| Vietnamese    | Tạo tài khoản  | Xem các mẫu      | Đăng nhập |
| English       | Create account | Browse templates | Sign in   |

Signed-in actions are “Mở CV của bạn” and “Xem các mẫu” in Vietnamese, and “Open
your resumes” and “Browse templates” in English.

## Supporting copy

| Site language | Fact                    | Explanation                                                                 |
| ------------- | ----------------------- | --------------------------------------------------------------------------- |
| Vietnamese    | Của bạn, do bạn giữ.    | Tối đa ba CV mỗi tài khoản, riêng tư cho đến khi bạn đăng.                  |
| Vietnamese    | Mỗi CV một đường dẫn.   | Đăng, gỡ đăng và cho phép lập chỉ mục tìm kiếm riêng cho từng CV.           |
| Vietnamese    | Dùng trợ lý AI của bạn. | Kết nối trợ lý hỗ trợ MCP với các quyền do bạn cấp và có thể thu hồi.       |
| English       | Yours to keep.          | Up to three resumes per account, private until you publish.                 |
| English       | One link per resume.    | Publish, unpublish, and control search indexing for each resume on its own. |
| English       | Bring your own agent.   | Connect an MCP-capable assistant with scopes you grant and can revoke.      |

The publish section heading is “Đăng CV gồm ba lựa chọn” in Vietnamese and
“Publishing is three choices” in English.

| Site language | Choice        | Explanation                                                                         |
| ------------- | ------------- | ----------------------------------------------------------------------------------- |
| Vietnamese    | CV công khai  | Có trang công khai hay không.                                                       |
| Vietnamese    | Tải PDF       | Người xem có được tải PDF hay không. Bạn luôn có thể tự xuất bản của mình.          |
| Vietnamese    | SEO và GEO    | Công cụ tìm kiếm và công cụ trả lời AI có được lập chỉ mục hay không. Mặc định tắt. |
| English       | Public resume | Whether any public page exists.                                                     |
| English       | PDF download  | Whether visitors can download the PDF. You can always export your own.              |
| English       | SEO and GEO   | Whether search engines and AI answer engines may index it. Off by default.          |

## Direction contract

The resume is a document and publishing is stamping it: a round red seal at the
sheet's foot, pressed by a person. The hero contains one rendered document,
rather than a template carousel or profile card.

The world uses a white bond sheet on a cool grey desk (`#EDEFEB`), ink
`#171A18`, pencil grey `#5F6763`, hairline rules `#D8DDD9`, seal red `#C8102E`,
and signature blue-black `#1F2A44`. Dark theme uses the `#121614` desk and
`#1A1F1C` panels; the sheet stays white. Application chrome uses Be Vietnam Pro.
Uppercase appears only inside the seal. Saved and draft states use pencil marks;
public state uses the seal. Chrome follows an 8 px module.

A visitor sees a finished resume with a seal naming its public link, understands
that the document is public while the account stays private, and creates an
account. Publish later repeats the stamping action in the editor.

At widths from 42 rem, the hero uses twelve columns. Copy spans five columns and
starts 96 px below the hero top; the sample spans seven. The compiled-in sample
sits in a 210 mm by at least 297 mm white container at `zoom: 0.6`, with a paper
shadow. A 96 px seal sits at the lower right, rotated -8 degrees, with ring text
“PUBLIC RESUME · ABOUTME.VN/ADA-LOVELACE”. The three facts form one ruled row,
followed by the three publish choices in another ruled row.

Below 42 rem, the hero becomes one column and the sheet uses `zoom: 0.5`. At 390
px and below, it uses `zoom: 0.44`. The seal moves to 30% from the sheet's left
edge so it lands on the main column's blank foot.

## Signature interaction and motion

Publish in the editor stamps: on success the seal lands on the preview sheet's
foot in one 180 ms press from scale 1.12 to 1, with the ink fading in. Unpublish
lifts it in 120 ms. Reduced motion makes both changes instant. The landing page
does not animate on load.

## Cross-surface reach

- Resume list: up to three sheets on the desk; each shows title, updated time,
  and a seal mark with its link or a pencil “Draft”. Empty slots are faint
  outlines. Create resume in the page header is the only create control. Rename
  and Delete sit in an overflow menu.
- Editor: four regions on the desk, sheet white in both themes; Publish is the
  only red control; Saved is a pencil tick; a small seal sits beside the title
  when public; fields commit on blur with human labels; the outline uses section
  icons.
- Publish dialog: the slug field shows an `aboutme.vn/` prefix; three switches
  carry their explanations; optional fields set the browser-tab title and emoji
  icon; Publish uses seal red; success shows the seal and Copy link.
- Auth pages: the form sits on the desk without a card and uses a left-aligned
  title.
- Settings: sections are divided by rules; devices show a description such as
  “Chrome on Linux”, relative last-seen time, and “This device”. Password and
  privacy sections always render. Connected agents and sign-in providers render
  when their capabilities are available.

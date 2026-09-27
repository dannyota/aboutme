// Copy for /app/import/linkedin (docs/design/linkedin-import-ui.md Copy).
// `importLinkedIn`, the create-dialog entry link text, lives in
// i18n/resume-create.ts next to the dialog's other copy.
import type { WorkspaceCopy } from './workspace';

// noticeCut's {max} is a character count, formatted per locale's own
// grouping (docs/design/linkedin-import-ui.md, "Messages").
const enNumberFormat = new Intl.NumberFormat('en-US');
const viNumberFormat = new Intl.NumberFormat('vi-VN');

export type ImportCopy = {
  readonly documentTitle: string;
  readonly back: string;
  readonly heading: string;
  readonly lead: string;
  readonly stepsHeading: string;
  readonly step1: string;
  readonly step2: string;
  readonly step3: string;
  readonly step4: string;
  readonly englishOnly: string;
  readonly pickHeading: string;
  readonly dropPrompt: string;
  readonly dropActive: string;
  readonly or: string;
  readonly choose: string;
  readonly chooseAnother: string;
  readonly limits: string;
  readonly privacy: string;
  readonly ownProfile: string;
  readonly reading: string;
  readonly readingPage: (n: number, m: number) => string;
  readonly readingNote: string;
  readonly progressLabel: string;
  readonly stop: string;
  readonly stopLabel: string;
  readonly stopped: string;
  readonly notPdf: string;
  readonly notLinkedIn: string;
  readonly notEnglish: string;
  readonly unreadable: string;
  readonly encrypted: string;
  readonly tooLarge: string;
  readonly tooManyPages: string;
  readonly timedOut: string;
  readonly dropOne: string;
  readonly oldBrowser: string;
  readonly readerFailed: string;
  readonly reviewHeading: string;
  readonly reviewLead: string;
  readonly noticesHeading: string;
  readonly noticeDates: (entry: string) => string;
  readonly noticeStartOnly: (entry: string) => string;
  readonly noticeCut: (field: string, max: number) => string;
  readonly noticeOverLimit: (section: string) => string;
  readonly resumeHeading: string;
  readonly defaultTitle: string;
  readonly languageLine: string;
  readonly templateLine: (name: string) => string;
  readonly contactOffHint: string;
  readonly groupLabel: (section: string) => string;
  readonly groupCount: (n: number, m: number) => string;
  readonly summaryEntry: string;
  readonly entryNoDates: string;
  readonly entryCut: string;
  readonly entryInvalid: string;
  readonly checkLine: string;
  readonly notImportedHeading: string;
  readonly notImportedLine: (section: string, n: number) => string;
  readonly notImportedNote: string;
  readonly panelHeading: string;
  readonly selectedCount: (n: number, s: number) => string;
  readonly sizeLabel: string;
  readonly sizeValue: (n: number, max: number) => string;
  readonly sizeOver: (max: number) => string;
  readonly sizeOk: string;
  readonly invalid: string;
  /** A schema failure outside any entry or detail: no row to deselect, so
   * this names the whole document instead (docs/design/
   * linkedin-import-ui.md, "Action panel"). */
  readonly invalidGeneral: string;
};

export const importCopy: WorkspaceCopy<ImportCopy> = {
  vi: {
    documentTitle: 'Nhập từ LinkedIn',
    back: 'Quay lại CV của bạn',
    heading: 'Nhập từ LinkedIn',
    lead: 'Tạo CV mới từ tệp PDF hồ sơ LinkedIn của bạn. Bạn kiểm tra những gì '
      + 'đọc được trước khi lưu.',
    stepsHeading: 'Lấy tệp PDF hồ sơ',
    step1: 'Trên máy tính, mở hồ sơ của bạn trên linkedin.com. Ứng dụng '
      + 'LinkedIn trên điện thoại không lưu được PDF.',
    step2: 'Đặt ngôn ngữ LinkedIn sang tiếng Anh và kiểm tra hồ sơ của bạn '
      + 'được viết bằng tiếng Anh.',
    step3: 'Trên trang hồ sơ, chọn More, rồi Save to PDF.',
    step4: 'Chọn tệp vừa tải về ở bên dưới.',
    englishOnly: 'Chỉ hồ sơ tiếng Anh dùng được, vì LinkedIn chỉ lưu văn bản '
      + 'tiếng Anh vào PDF.',
    pickHeading: 'Chọn tệp PDF',
    dropPrompt: 'Thả tệp PDF LinkedIn vào đây',
    dropActive: 'Thả ra để đọc tệp này',
    or: 'hoặc',
    choose: 'Chọn tệp PDF',
    chooseAnother: 'Chọn tệp PDF khác',
    limits: 'Một tệp PDF, tối đa 4 MB và 20 trang.',
    privacy: 'Tệp không rời khỏi thiết bị này: trình duyệt của bạn tự đọc '
      + 'tệp, aboutme không nhận tệp. Chúng tôi chỉ lưu CV bạn tạo.',
    ownProfile: 'Hãy nhập hồ sơ của chính bạn, không phải của người khác.',
    reading: 'Đang đọc tệp PDF…',
    readingPage: (n, m) => `Trang ${n}/${m}`,
    readingNote: 'Việc này mất vài giây. Sau 15 giây, chúng tôi sẽ dừng.',
    progressLabel: 'Tiến độ đọc',
    stop: 'Dừng',
    stopLabel: 'Dừng đọc tệp PDF',
    stopped: 'Đã dừng. Hãy chọn tệp PDF để thử lại.',
    notPdf: 'Tệp này không phải PDF. Hãy chọn tệp PDF mà LinkedIn đã lưu.',
    notLinkedIn: 'Đây không phải tệp PDF hồ sơ LinkedIn. Hãy mở hồ sơ trên '
      + 'LinkedIn, chọn More, rồi Save to PDF.',
    notEnglish: 'Tệp PDF LinkedIn này không phải tiếng Anh. LinkedIn chỉ lưu '
      + 'hồ sơ tiếng Anh vào PDF. Hãy đặt ngôn ngữ LinkedIn sang tiếng Anh '
      + 'rồi lưu lại PDF.',
    unreadable: 'Không đọc được chữ trong tệp này. Hãy lưu lại PDF từ '
      + 'LinkedIn rồi chọn tệp mới.',
    encrypted: 'Tệp PDF này có mật khẩu. PDF hồ sơ LinkedIn không có mật '
      + 'khẩu, nên hãy lưu lại từ LinkedIn.',
    tooLarge: 'Tệp này quá lớn để nhập. PDF hồ sơ LinkedIn nhỏ hơn nhiều; '
      + 'hãy kiểm tra bạn đã chọn đúng tệp.',
    tooManyPages: 'Tệp PDF này có hơn 20 trang. Hãy chọn tệp PDF mà LinkedIn '
      + 'lưu từ hồ sơ của bạn.',
    timedOut: 'Đọc tệp này mất hơn 15 giây nên chúng tôi đã dừng. Hãy thử '
      + 'lại, hoặc lưu lại PDF từ LinkedIn.',
    dropOne: 'Mỗi lần chỉ thả một tệp.',
    oldBrowser: 'Trình duyệt này không đọc được PDF trên trang này. Hãy dùng '
      + 'phiên bản mới của Chrome, Edge, Firefox hoặc Safari.',
    readerFailed: 'Không tải được trình đọc PDF. Hãy kiểm tra kết nối rồi '
      + 'tải lại trang.',
    reviewHeading: 'Kiểm tra dữ liệu nhập',
    reviewLead: 'Chọn những gì đưa vào CV mới. Chưa có gì được lưu cho đến '
      + 'khi bạn tạo CV.',
    noticesHeading: 'Kiểm tra trước khi tạo',
    noticeDates: (entry) =>
      `${entry}: không đọc được thời gian. Hãy thêm trong trình chỉnh sửa.`,
    noticeStartOnly: (entry) => `${entry}: chỉ tìm thấy ngày bắt đầu. Hãy `
      + 'thêm ngày kết thúc trong trình chỉnh sửa.',
    noticeCut: (field, max) =>
      `${field} đã được rút gọn còn ${viNumberFormat.format(max)} ký tự.`,
    noticeOverLimit: (section) => `${section} có hơn 64 mục. 64 mục đầu đã `
      + 'được chọn; các mục còn lại không được chọn.',
    resumeHeading: 'CV',
    defaultTitle: 'CV từ LinkedIn',
    languageLine: 'Ngôn ngữ: tiếng Anh, giống hồ sơ LinkedIn của bạn.',
    templateLine: (name) =>
      `Mẫu: ${name}. Bạn có thể đổi mẫu trong trình chỉnh sửa.`,
    contactOffHint: 'Mặc định không chọn, vì CV đã đăng sẽ hiển thị thông '
      + 'tin này.',
    groupLabel: (section) => `Nhập toàn bộ ${section}`,
    groupCount: (n, m) => `Đã chọn ${n}/${m}`,
    summaryEntry: 'Đoạn tóm tắt hồ sơ',
    entryNoDates: 'Chưa có thời gian',
    entryCut: 'Đã rút gọn',
    entryInvalid: 'Không lưu được như hiện tại. Hãy bỏ chọn, hoặc sửa sau '
      + 'trong trình chỉnh sửa.',
    checkLine: 'Hãy kiểm tra từng mục. Bố cục PDF có thể tách hoặc nối nhầm '
      + 'dòng; bạn có thể sửa trong trình chỉnh sửa.',
    notImportedHeading: 'Không được nhập',
    notImportedLine: (section, n) => `${section} (${n})`,
    notImportedNote: 'aboutme không nhập các phần này. Bạn có thể tự thêm '
      + 'trong trình chỉnh sửa nếu cần.',
    panelHeading: 'CV mới của bạn',
    selectedCount: (n, s) => `${n} mục từ ${s} phần`,
    sizeLabel: 'Dung lượng CV',
    sizeValue: (n, max) => `${n} KB / ${max} KB`,
    sizeOver: (max) => `CV quá lớn để tạo. Hãy bỏ chọn bớt mục để dưới `
      + `${max} KB.`,
    sizeOk: 'CV đã vừa dung lượng cho phép.',
    invalid: 'Một số mục không lưu được như hiện tại. Hãy bỏ chọn các mục '
      + 'được đánh dấu.',
    invalidGeneral: 'Không lưu được CV này như hiện tại. Hãy quay lại chọn '
      + 'tệp PDF khác, hoặc tạo CV trống.',
  },
  en: {
    documentTitle: 'Import from LinkedIn',
    back: 'Back to your resumes',
    heading: 'Import from LinkedIn',
    lead: 'Create a new resume from your LinkedIn profile PDF. You check '
      + 'what we found before anything is saved.',
    stepsHeading: 'Get your profile PDF',
    step1: 'On a computer, open your profile on linkedin.com. The LinkedIn '
      + 'phone app cannot save a PDF.',
    step2: 'Set LinkedIn\'s language to English, and check that your '
      + 'profile is written in English.',
    step3: 'On your profile, choose More, then Save to PDF.',
    step4: 'Choose the downloaded file below.',
    englishOnly: 'Only English profiles work, because LinkedIn saves only '
      + 'English text to PDF.',
    pickHeading: 'Choose the PDF',
    dropPrompt: 'Drop your LinkedIn PDF here',
    dropActive: 'Release to read this file',
    or: 'or',
    choose: 'Choose PDF',
    chooseAnother: 'Choose another PDF',
    limits: 'One PDF, up to 4 MB and 20 pages.',
    privacy: 'The file stays on this device: your browser reads it, and '
      + 'aboutme never receives it. We store only the resume you create.',
    ownProfile: 'Import your own profile, not someone else\'s.',
    reading: 'Reading your PDF…',
    readingPage: (n, m) => `Page ${n} of ${m}`,
    readingNote: 'This takes a few seconds. We stop after 15 seconds.',
    progressLabel: 'Reading progress',
    stop: 'Stop',
    stopLabel: 'Stop reading the PDF',
    stopped: 'Stopped. Choose a PDF to try again.',
    notPdf: 'This file is not a PDF. Choose the PDF that LinkedIn saved.',
    notLinkedIn: 'This is not a LinkedIn profile PDF. Open your profile on '
      + 'LinkedIn, choose More, then Save to PDF.',
    notEnglish: 'This LinkedIn PDF is not in English. LinkedIn saves only '
      + 'English profiles to PDF. Set LinkedIn\'s language to English and '
      + 'save the PDF again.',
    unreadable: 'We cannot read the text in this file. Save the PDF from '
      + 'LinkedIn again and choose the new file.',
    encrypted: 'This PDF is locked with a password. LinkedIn profile PDFs '
      + 'have none, so save yours from LinkedIn again.',
    tooLarge: 'This file is too large to import. A LinkedIn profile PDF is '
      + 'much smaller; check that you chose the right file.',
    tooManyPages: 'This PDF has more than 20 pages. Choose the PDF that '
      + 'LinkedIn saved from your profile.',
    timedOut: 'Reading this file took longer than 15 seconds, so we '
      + 'stopped. Try again, or save the PDF from LinkedIn again.',
    dropOne: 'Drop one file at a time.',
    oldBrowser: 'This browser cannot read PDFs on this page. Use a recent '
      + 'version of Chrome, Edge, Firefox, or Safari.',
    readerFailed: 'The PDF reader did not load. Check your connection and '
      + 'reload the page.',
    reviewHeading: 'Check your import',
    reviewLead: 'Choose what goes into your new resume. Nothing is saved '
      + 'until you create it.',
    noticesHeading: 'Check before you create',
    noticeDates: (entry) =>
      `${entry}: we could not read the dates. Add them in the editor.`,
    noticeStartOnly: (entry) => `${entry}: only a start date was found. Add `
      + 'the end in the editor.',
    noticeCut: (field, max) =>
      `${field} was shortened to ${enNumberFormat.format(max)} characters.`,
    noticeOverLimit: (section) => `${section} has more than 64 entries. The `
      + 'first 64 are selected; the rest stay off.',
    resumeHeading: 'Resume',
    defaultTitle: 'LinkedIn resume',
    languageLine: 'Language: English, like your LinkedIn profile.',
    templateLine: (name) =>
      `Template: ${name}. You can change it in the editor.`,
    contactOffHint: 'Off at first, because a published resume shows it.',
    groupLabel: (section) => `Import all of ${section}`,
    groupCount: (n, m) => `${n} of ${m} selected`,
    summaryEntry: 'Profile summary',
    entryNoDates: 'No dates',
    entryCut: 'Shortened',
    entryInvalid: 'Cannot be saved as is. Deselect it or fix it in the '
      + 'editor later.',
    checkLine: 'Check each entry. The PDF layout can split or join lines; '
      + 'you can fix them in the editor.',
    notImportedHeading: 'Not imported',
    notImportedLine: (section, n) => `${section} (${n})`,
    notImportedNote: 'aboutme does not import these sections. Add them in '
      + 'the editor if you need them.',
    panelHeading: 'Your new resume',
    selectedCount: (n, s) =>
      `${n} ${n === 1 ? 'item' : 'items'} from `
      + `${s} ${s === 1 ? 'section' : 'sections'}`,
    sizeLabel: 'Resume size',
    sizeValue: (n, max) => `${n} KB of ${max} KB`,
    sizeOver: (max) =>
      `Too large to create. Deselect some entries to go under ${max} KB.`,
    sizeOk: 'The resume fits again.',
    invalid: 'Some entries cannot be saved as they are. Deselect the marked '
      + 'entries.',
    invalidGeneral: 'This resume cannot be saved as it is. Go back and '
      + 'choose another PDF, or create a blank resume.',
  },
};

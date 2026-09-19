import type { WorkspaceCopy } from './workspace';

type EntryCopy = {
  readonly profile: Record<'text', string>;
  readonly work: Record<
    | 'jobTitle'
    | 'employer'
    | 'employerLink'
    | 'city'
    | 'country'
    | 'description',
    string
  >;
  readonly education: Record<
    'degree' | 'school' | 'schoolLink' | 'city' | 'country' | 'description',
    string
  >;
  readonly skill: Record<'name' | 'level' | 'infoHtml', string>;
  readonly language: Record<'name' | 'level', string>;
  readonly certificate: Record<
    'title' | 'titleLink' | 'issuer' | 'date' | 'description',
    string
  >;
  readonly project: Record<
    'title' | 'subtitle' | 'link' | 'description',
    string
  >;
  readonly custom: Record<
    'title' | 'titleLink' | 'subtitle' | 'city' | 'description',
    string
  >;
};

export type EditorFieldsCopy = {
  readonly personal: Record<
    | 'title'
    | 'fullName'
    | 'headline'
    | 'contactDetails'
    | 'contactDetail'
    | 'type'
    | 'value'
    | 'label'
    | 'showAs'
    | 'addDetail'
    | 'removeContactList'
    | 'contactLimit'
    | 'urlError'
    | 'moreOptions'
    | 'setLabel'
    | 'hideDetail'
    | 'moveUp'
    | 'moveDown'
    | 'removeDetail'
    | 'shortAddress'
    | 'fullAddress'
    | 'labelDisplay'
    | 'labelHint'
    | 'email'
    | 'phone'
    | 'location'
    | 'website'
    | 'linkedin'
    | 'github'
    | 'twitter'
    | 'custom',
    string
  >;
  readonly language: Record<
    | 'resumeLanguage'
    | 'hint'
    | 'code'
    | 'other'
    | 'unset'
    | 'codeHint'
    | 'codeError',
    string
  >;
  readonly dates: Record<
    | 'startDate'
    | 'endDate'
    | 'dateRange'
    | 'startYear'
    | 'startMonth'
    | 'endYear'
    | 'endMonth'
    | 'year'
    | 'month'
    | 'present'
    | 'remove'
    | 'invalidYearMonth'
    | 'invalidStart'
    | 'missingEnd'
    | 'order',
    string
  >;
  readonly links: Record<'invalid', string>;
  readonly entryCard: Record<
    'collapse' | 'expand' | 'hidden' | 'moveUp' | 'moveDown' | 'delete',
    string
  >;
  readonly section: Record<
    | 'addEntry'
    | 'issues'
    | 'deleteEntry'
    | 'delete'
    | 'entryChanged'
    | 'tooLong'
    | 'tooMany'
    | 'richTextTooLong'
    | 'format'
    | 'generic'
    | 'cancel',
    string
  > & { readonly deleteDescription: (entry: string) => string };
  readonly levels: {
    readonly unset: string;
    readonly skill: readonly string[];
    readonly language: readonly string[];
  };
  readonly entry: EntryCopy;
};

export const editorFieldsCopy: WorkspaceCopy<EditorFieldsCopy> = {
  vi: {
    personal: {
      title: 'Thông tin cá nhân',
      fullName: 'Họ và tên',
      headline: 'Tiêu đề',
      contactDetails: 'Thông tin liên hệ',
      contactDetail: 'Thông tin liên hệ',
      type: 'Loại',
      value: 'Giá trị',
      label: 'Nhãn',
      showAs: 'Hiển thị',
      addDetail: 'Thêm thông tin',
      removeContactList: 'Xóa danh sách liên hệ',
      contactLimit: 'Bạn có thể thêm tối đa 16 thông tin liên hệ.',
      urlError: 'Dùng URL https:// viết thường.',
      moreOptions: 'Tùy chọn khác cho thông tin liên hệ',
      setLabel: 'Đặt nhãn…',
      hideDetail: 'Ẩn thông tin này',
      moveUp: 'Di chuyển lên',
      moveDown: 'Di chuyển xuống',
      removeDetail: 'Xóa thông tin',
      shortAddress: 'Địa chỉ ngắn',
      fullAddress: 'Địa chỉ đầy đủ',
      labelDisplay: 'Nhãn',
      labelHint: 'Nhãn hiển thị nhãn của thông tin, hoặc tên loại.',
      email: 'Email',
      phone: 'Điện thoại',
      location: 'Địa điểm',
      website: 'Trang web',
      linkedin: 'LinkedIn',
      github: 'GitHub',
      twitter: 'X (Twitter)',
      custom: 'Tùy chỉnh',
    },
    language: {
      resumeLanguage: 'Ngôn ngữ CV',
      hint: 'Ngôn ngữ CV của bạn được viết bằng.',
      code: 'Mã ngôn ngữ',
      other: 'Khác…',
      unset: 'Chưa đặt',
      codeHint: 'Nhập mã ngôn ngữ, ví dụ fr hoặc zh-Hant.',
      codeError: 'Nhập mã ngôn ngữ hợp lệ, ví dụ fr hoặc zh-Hant.',
    },
    dates: {
      startDate: 'Ngày bắt đầu',
      endDate: 'Ngày kết thúc',
      dateRange: 'Khoảng thời gian',
      startYear: 'Năm bắt đầu',
      startMonth: 'Tháng bắt đầu',
      endYear: 'Năm kết thúc',
      endMonth: 'Tháng kết thúc',
      year: 'Năm',
      month: 'Tháng',
      present: 'Hiện tại',
      remove: 'Xóa ngày',
      invalidYearMonth: 'Nhập năm và tháng hợp lệ.',
      invalidStart: 'Nhập ngày bắt đầu hợp lệ.',
      missingEnd: 'Thêm ngày kết thúc hoặc chọn Hiện tại để lưu ngày này.',
      order: 'Ngày bắt đầu không được sau ngày kết thúc.',
    },
    links: {
      invalid: 'Nhập liên kết https://, hoặc địa chỉ mailto: hay tel:.',
    },
    entryCard: {
      collapse: 'Thu gọn trường mục',
      expand: 'Mở rộng trường mục',
      hidden: 'Ẩn',
      moveUp: 'Di chuyển mục lên',
      moveDown: 'Di chuyển mục xuống',
      delete: 'Xóa mục',
    },
    section: {
      addEntry: 'Thêm mục',
      issues: 'Vấn đề của phần',
      deleteEntry: 'Xóa mục',
      deleteDescription: (entry) => `Xóa ${entry}?`,
      delete: 'Xóa',
      entryChanged: 'Mục đã thay đổi. Mở lại xác nhận xóa.',
      tooLong: 'Giá trị này quá dài.',
      tooMany: 'Phần này có quá nhiều mục.',
      richTextTooLong: 'Văn bản có định dạng quá dài.',
      format: 'Nhập giá trị theo định dạng yêu cầu.',
      generic: 'Giá trị này cần được kiểm tra.',
      cancel: 'Hủy',
    },
    levels: {
      unset: 'Chưa đặt',
      skill: [
        'Không có',
        'Mới bắt đầu',
        'Cơ bản',
        'Trung cấp',
        'Nâng cao',
        'Chuyên gia',
      ],
      language: [
        'Không có',
        'Sơ cấp',
        'Làm việc hạn chế',
        'Làm việc chuyên nghiệp',
        'Thành thạo',
        'Bản ngữ hoặc song ngữ',
      ],
    },
    entry: {
      profile: { text: 'Nội dung hồ sơ' },
      work: {
        jobTitle: 'Chức danh',
        employer: 'Công ty',
        employerLink: 'Liên kết công ty',
        city: 'Thành phố',
        country: 'Quốc gia',
        description: 'Mô tả công việc',
      },
      education: {
        degree: 'Bằng cấp',
        school: 'Trường',
        schoolLink: 'Liên kết trường',
        city: 'Thành phố',
        country: 'Quốc gia',
        description: 'Mô tả học vấn',
      },
      skill: { name: 'Tên', level: 'Cấp độ', infoHtml: 'Thông tin kỹ năng' },
      language: { name: 'Tên', level: 'Cấp độ' },
      certificate: {
        title: 'Tiêu đề',
        titleLink: 'Liên kết tiêu đề',
        issuer: 'Tổ chức cấp',
        date: 'Ngày',
        description: 'Mô tả chứng chỉ',
      },
      project: {
        title: 'Tiêu đề',
        subtitle: 'Phụ đề',
        link: 'Liên kết dự án',
        description: 'Mô tả dự án',
      },
      custom: {
        title: 'Tiêu đề',
        titleLink: 'Liên kết tiêu đề',
        subtitle: 'Phụ đề',
        city: 'Thành phố',
        description: 'Mô tả tùy chỉnh',
      },
    },
  },
  en: {
    personal: {
      title: 'Personal details',
      fullName: 'Full name',
      headline: 'Headline',
      contactDetails: 'Contact details',
      contactDetail: 'Contact detail',
      type: 'Type',
      value: 'Value',
      label: 'Label',
      showAs: 'Show as',
      addDetail: 'Add detail',
      removeContactList: 'Remove contact list',
      contactLimit: 'You can add up to 16 contact details.',
      urlError: 'Use a lowercase https:// URL.',
      moreOptions: 'More options for contact detail',
      setLabel: 'Set label…',
      hideDetail: 'Hide this detail',
      moveUp: 'Move up',
      moveDown: 'Move down',
      removeDetail: 'Remove detail',
      shortAddress: 'Short address',
      fullAddress: 'Full address',
      labelDisplay: 'Label',
      labelHint: 'Label shows the detail\'s label, or the type\'s name.',
      email: 'Email',
      phone: 'Phone',
      location: 'Location',
      website: 'Website',
      linkedin: 'LinkedIn',
      github: 'GitHub',
      twitter: 'X (Twitter)',
      custom: 'Custom',
    },
    language: {
      resumeLanguage: 'Resume language',
      hint: 'The language your resume is written in.',
      code: 'Language code',
      other: 'Other…',
      unset: 'Not set',
      codeHint: 'Enter a language code, such as fr or zh-Hant.',
      codeError: 'Enter a valid language code, such as fr or zh-Hant.',
    },
    dates: {
      startDate: 'Start date',
      endDate: 'End date',
      dateRange: 'Date range',
      startYear: 'Start year',
      startMonth: 'Start month',
      endYear: 'End year',
      endMonth: 'End month',
      year: 'Year',
      month: 'Month',
      present: 'Present',
      remove: 'Remove date',
      invalidYearMonth: 'Enter a valid year and month.',
      invalidStart: 'Enter a valid start date.',
      missingEnd: 'Add an end date or tick Present to save this date.',
      order: 'Start date must not be after end date.',
    },
    links: { invalid: 'Enter an https:// link, or a mailto: or tel: address.' },
    entryCard: {
      collapse: 'Collapse entry fields',
      expand: 'Expand entry fields',
      hidden: 'Hidden',
      moveUp: 'Move entry up',
      moveDown: 'Move entry down',
      delete: 'Delete entry',
    },
    section: {
      addEntry: 'Add entry',
      issues: 'Section issues',
      deleteEntry: 'Delete entry',
      deleteDescription: (entry) => `Delete ${entry}?`,
      delete: 'Delete',
      entryChanged: 'Entry changed. Reopen delete confirmation.',
      tooLong: 'This value is too long.',
      tooMany: 'There are too many entries in this section.',
      richTextTooLong: 'Rich text is too long.',
      format: 'Enter a value in the required format.',
      generic: 'This value needs attention.',
      cancel: 'Cancel',
    },
    levels: {
      unset: 'Not set',
      skill: [
        'None',
        'Beginner',
        'Basic',
        'Intermediate',
        'Advanced',
        'Expert',
      ],
      language: [
        'None',
        'Elementary',
        'Limited working',
        'Professional working',
        'Full professional',
        'Native or bilingual',
      ],
    },
    entry: {
      profile: { text: 'Profile text' },
      work: {
        jobTitle: 'Job title',
        employer: 'Employer',
        employerLink: 'Employer link',
        city: 'City',
        country: 'Country',
        description: 'Work description',
      },
      education: {
        degree: 'Degree',
        school: 'School',
        schoolLink: 'School link',
        city: 'City',
        country: 'Country',
        description: 'Education description',
      },
      skill: { name: 'Name', level: 'Level', infoHtml: 'Skill information' },
      language: { name: 'Name', level: 'Level' },
      certificate: {
        title: 'Title',
        titleLink: 'Title link',
        issuer: 'Issuer',
        date: 'Date',
        description: 'Certificate description',
      },
      project: {
        title: 'Title',
        subtitle: 'Subtitle',
        link: 'Project link',
        description: 'Project description',
      },
      custom: {
        title: 'Title',
        titleLink: 'Title link',
        subtitle: 'Subtitle',
        city: 'City',
        description: 'Custom description',
      },
    },
  },
};

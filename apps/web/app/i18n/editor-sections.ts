import type { Section } from '@aboutme/schema';
import type { Locale } from './locale';
import type { WorkspaceCopy } from './workspace';

type SectionType = Section['sectionType'];

export const sectionIconKeys = [
  '', 'user', 'briefcase', 'graduation-cap', 'code', 'languages', 'award',
  'folder', 'trophy', 'globe', 'badge-check', 'book-open', 'bookmark',
  'building-2', 'camera', 'chart-line', 'cpu', 'compass', 'database',
  'file-text', 'dumbbell', 'flag', 'flask-conical', 'heart',
  'heart-handshake', 'landmark', 'leaf', 'library', 'lightbulb', 'link',
  'mail', 'map-pin', 'medal', 'megaphone', 'mic', 'microscope', 'music',
  'newspaper', 'palette', 'pen-tool', 'users', 'phone', 'plane',
  'presentation', 'puzzle', 'rocket', 'scale', 'school', 'scroll-text',
  'sparkles', 'star', 'stethoscope', 'target', 'terminal', 'trending-up',
  'wrench',
] as const;

export type SectionIconKey = (typeof sectionIconKeys)[number];

type EditorSectionsCopy = {
  readonly currentIcon: string;
  readonly entry: (index: number) => string;
  readonly iconLabels: Readonly<Record<SectionIconKey, string>>;
  readonly sectionTypes: Readonly<Record<SectionType, string>>;
};

const enIcons: Record<SectionIconKey, string> = {
  '': 'No icon',
  'user': 'Person',
  'briefcase': 'Briefcase',
  'graduation-cap': 'Graduation cap',
  'code': 'Code',
  'languages': 'Languages',
  'award': 'Award',
  'folder': 'Folder',
  'trophy': 'Trophy',
  'globe': 'Globe',
  'badge-check': 'Badge',
  'book-open': 'Book',
  'bookmark': 'Bookmark',
  'building-2': 'Building',
  'camera': 'Camera',
  'chart-line': 'Chart',
  'cpu': 'Chip',
  'compass': 'Compass',
  'database': 'Database',
  'file-text': 'Document',
  'dumbbell': 'Dumbbell',
  'flag': 'Flag',
  'flask-conical': 'Flask',
  'heart': 'Heart',
  'heart-handshake': 'Helping hands',
  'landmark': 'Landmark',
  'leaf': 'Leaf',
  'library': 'Library',
  'lightbulb': 'Light bulb',
  'link': 'Link',
  'mail': 'Mail',
  'map-pin': 'Map pin',
  'medal': 'Medal',
  'megaphone': 'Megaphone',
  'mic': 'Microphone',
  'microscope': 'Microscope',
  'music': 'Music',
  'newspaper': 'Newspaper',
  'palette': 'Palette',
  'pen-tool': 'Pen',
  'users': 'People',
  'phone': 'Phone',
  'plane': 'Plane',
  'presentation': 'Presentation',
  'puzzle': 'Puzzle',
  'rocket': 'Rocket',
  'scale': 'Scale',
  'school': 'School',
  'scroll-text': 'Scroll',
  'sparkles': 'Sparkles',
  'star': 'Star',
  'stethoscope': 'Stethoscope',
  'target': 'Target',
  'terminal': 'Terminal',
  'trending-up': 'Trend',
  'wrench': 'Wrench',
};

const viIcons: Record<SectionIconKey, string> = {
  '': 'Không có biểu tượng',
  'user': 'Người',
  'briefcase': 'Cặp',
  'graduation-cap': 'Mũ tốt nghiệp',
  'code': 'Mã',
  'languages': 'Ngôn ngữ',
  'award': 'Giải thưởng',
  'folder': 'Thư mục',
  'trophy': 'Cúp',
  'globe': 'Quả địa cầu',
  'badge-check': 'Huy hiệu',
  'book-open': 'Sách',
  'bookmark': 'Dấu trang',
  'building-2': 'Tòa nhà',
  'camera': 'Máy ảnh',
  'chart-line': 'Biểu đồ',
  'cpu': 'Bộ xử lý',
  'compass': 'La bàn',
  'database': 'Cơ sở dữ liệu',
  'file-text': 'Tài liệu',
  'dumbbell': 'Tạ',
  'flag': 'Cờ',
  'flask-conical': 'Bình thí nghiệm',
  'heart': 'Trái tim',
  'heart-handshake': 'Bắt tay',
  'landmark': 'Địa danh',
  'leaf': 'Lá',
  'library': 'Thư viện',
  'lightbulb': 'Bóng đèn',
  'link': 'Liên kết',
  'mail': 'Thư',
  'map-pin': 'Ghim bản đồ',
  'medal': 'Huy chương',
  'megaphone': 'Loa',
  'mic': 'Micrô',
  'microscope': 'Kính hiển vi',
  'music': 'Âm nhạc',
  'newspaper': 'Báo',
  'palette': 'Bảng màu',
  'pen-tool': 'Bút',
  'users': 'Mọi người',
  'phone': 'Điện thoại',
  'plane': 'Máy bay',
  'presentation': 'Trình bày',
  'puzzle': 'Trò ghép hình',
  'rocket': 'Tên lửa',
  'scale': 'Cân',
  'school': 'Trường học',
  'scroll-text': 'Cuộn giấy',
  'sparkles': 'Lấp lánh',
  'star': 'Ngôi sao',
  'stethoscope': 'Ống nghe',
  'target': 'Mục tiêu',
  'terminal': 'Thiết bị đầu cuối',
  'trending-up': 'Xu hướng',
  'wrench': 'Cờ lê',
};

export const editorSectionsCopy: WorkspaceCopy<EditorSectionsCopy> = {
  vi: {
    currentIcon: 'Biểu tượng hiện tại',
    entry: (index) => `Mục ${index + 1}`,
    iconLabels: viIcons,
    sectionTypes: {
      profile: 'Hồ sơ', work: 'Kinh nghiệm làm việc', education: 'Học vấn',
      skill: 'Kỹ năng', language: 'Ngôn ngữ', certificate: 'Chứng chỉ',
      project: 'Dự án', custom: 'Phần tùy chỉnh',
    },
  },
  en: {
    currentIcon: 'Current icon',
    entry: (index) => `Entry ${index + 1}`,
    iconLabels: enIcons,
    sectionTypes: {
      profile: 'Profile', work: 'Work experience', education: 'Education',
      skill: 'Skills', language: 'Languages', certificate: 'Certifications',
      project: 'Projects', custom: 'Custom section',
    },
  },
};

export function sectionTypeLabelsForLocale(
  locale: Locale,
): Readonly<Record<SectionType, string>> {
  return editorSectionsCopy[locale].sectionTypes;
}

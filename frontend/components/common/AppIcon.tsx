import type { LucideIcon, LucideProps } from "lucide-react";
import {
  AtSign,
  BadgeCheck,
  Bell,
  Cat,
  CircleAlert,
  CirclePlay,
  Clapperboard,
  CloudUpload,
  Flame,
  Globe,
  House,
  Image,
  ImagePlus,
  Import as ImportIcon,
  Info,
  LayoutGrid,
  MessageCircle,
  MessageSquare,
  PenSquare,
  Play,
  RefreshCw,
  Search,
  Share2,
  Smile,
  Star,
  Tag,
  ThumbsUp,
  Upload,
  Magnet,
  User,
  UserCircle,
  Users,
  X,
  Eye,
  CalendarDays,
} from "lucide-react";
import type { ComponentProps } from "react";

export type IconName =
  | "sentiment_neutral"
  | "cloud_upload"
  | "edit_note"
  | "close"
  | "tag"
  | "image"
  | "add_photo_alternate"
  | "info"
  | "autorenew"
  | "error"
  | "visibility"
  | "calendar_today"
  | "thumb_up"
  | "star"
  | "share"
  | "verified"
  | "play_circle"
  | "play_arrow"
  | "chat_bubble"
  | "person"
  | "local_fire_department"
  | "face_5"
  | "public"
  | "alternate_email"
  | "chat"
  | "groups"
  | "movie_filter"
  | "notifications"
  | "account_circle"
  | "home"
  | "grid_view"
  | "search"
  | "upload"
  | "input"
  | "magnet";

const ICON_MAP: Record<IconName, LucideIcon> = {
  sentiment_neutral: Smile,
  cloud_upload: CloudUpload,
  edit_note: PenSquare,
  close: X,
  tag: Tag,
  image: Image,
  add_photo_alternate: ImagePlus,
  info: Info,
  autorenew: RefreshCw,
  error: CircleAlert,
  visibility: Eye,
  calendar_today: CalendarDays,
  thumb_up: ThumbsUp,
  star: Star,
  share: Share2,
  verified: BadgeCheck,
  play_circle: CirclePlay,
  play_arrow: Play,
  chat_bubble: MessageSquare,
  person: User,
  local_fire_department: Flame,
  face_5: Cat,
  public: Globe,
  alternate_email: AtSign,
  chat: MessageCircle,
  groups: Users,
  movie_filter: Clapperboard,
  notifications: Bell,
  account_circle: UserCircle,
  home: House,
  grid_view: LayoutGrid,
  search: Search,
  upload: Upload,
  input: ImportIcon,
  magnet: Magnet,
};

type AppIconProps = Omit<ComponentProps<"svg">, "name"> &
  Pick<LucideProps, "size"> & {
    name: IconName;
    strokeWidth?: number;
  };

export function AppIcon({ name, size = 24, strokeWidth = 1.75, ...props }: AppIconProps) {
  const Icon = ICON_MAP[name];
  return <Icon size={size} strokeWidth={strokeWidth} {...props} />;
}

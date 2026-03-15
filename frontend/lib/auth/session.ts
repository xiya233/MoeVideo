export type AuthUser = {
  id: string;
  username: string;
  email?: string;
  role?: string;
  bio?: string;
  avatar_url?: string;
  followers_count?: number;
  following_count?: number;
  profile_public?: boolean;
  public_videos?: boolean;
  public_favorites?: boolean;
  public_following?: boolean;
  public_followers?: boolean;
};

export type AuthSession = {
  user: AuthUser;
};


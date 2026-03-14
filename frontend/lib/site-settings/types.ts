export type FooterLink = {
  label: string;
  url: string;
};

export type FooterLinks = {
  about: FooterLink[];
  support: FooterLink[];
  legal: FooterLink[];
  updates: FooterLink[];
};

export type PublicSiteSettings = {
  site_title: string;
  site_description: string;
  site_logo_url?: string;
  footer_links: FooterLinks;
  register_enabled: boolean;
};

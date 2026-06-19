export interface BootstrapStatusResponse {
  initialized: boolean;
  message: string;
  /** Lodestar: true=商业模式(开放公开注册)，false/缺省=自用模式 */
  commercial_mode?: boolean;
  /** Lodestar: true=维护模式(对非管理员显示维护页) */
  maintenance_mode?: boolean;
  /** Lodestar: true=注册需邀请码(仅商业模式下) */
  register_invite_required?: boolean;
  /** Lodestar: true=注册需邮箱验证(仅商业模式下) */
  register_email_required?: boolean;
  site_banner_enabled?: boolean;
  site_banner_text?: string;
  site_banner_tone?: 'info' | 'warning' | 'success';
  /** Lodestar: true=GitHub OAuth login enabled */
  github_oauth_enabled?: boolean;
}

export interface BootstrapCreateAdminRequest {
  username: string;
  password: string;
}

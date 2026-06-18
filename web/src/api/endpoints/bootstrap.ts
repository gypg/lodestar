export interface BootstrapStatusResponse {
  initialized: boolean;
  message: string;
  /** GGZERO: true=商业模式(开放公开注册)，false/缺省=自用模式 */
  commercial_mode?: boolean;
  /** GGZERO: true=维护模式(对非管理员显示维护页) */
  maintenance_mode?: boolean;
}

export interface BootstrapCreateAdminRequest {
  username: string;
  password: string;
}

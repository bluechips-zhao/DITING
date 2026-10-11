import packageJson from "../../package.json";

const currentYear = new Date().getFullYear();

export const APP_CONFIG = {
  name: "谛听 DITING",
  version: packageJson.version,
  copyright: `© ${currentYear}, DITING.`,
  meta: {
    title: "谛听 DITING — 个人自主渗透助手",
    description: "LLM 驱动的个人自主渗透测试系统",
  },
};

import { createFsSkills } from "@atomyx/skills";
import { ArgvError, parseArgv } from "./argv.js";
import { printCommandHelp, printModuleHelp } from "./help.js";
import { runInit } from "./commands/init.js";
import { runUpdateSkills } from "./commands/update-skills.js";

export async function execute(args: readonly string[]): Promise<void> {
  const skills = createFsSkills();
  const command = args[0];

  switch (command) {
    case "init": {
      let argv;
      try {
        argv = parseArgv(args.slice(1), "init");
      } catch (err) {
        if (err instanceof ArgvError) {
          process.stderr.write(`error: ${err.message}\n\n`);
          printCommandHelp("init");
          process.exit(2);
          return;
        }
        throw err;
      }
      if (argv.help) {
        printCommandHelp("init", process.stdout.write.bind(process.stdout));
        return;
      }
      const code = await runInit(skills, argv.flags);
      if (code !== 0) process.exit(code);
      return;
    }
    case "update-skills": {
      let argv;
      try {
        argv = parseArgv(args.slice(1), "update-skills");
      } catch (err) {
        if (err instanceof ArgvError) {
          process.stderr.write(`error: ${err.message}\n\n`);
          printCommandHelp("update-skills");
          process.exit(2);
          return;
        }
        throw err;
      }
      if (argv.help) {
        printCommandHelp(
          "update-skills",
          process.stdout.write.bind(process.stdout),
        );
        return;
      }
      const code = await runUpdateSkills(skills, argv.flags);
      if (code !== 0) process.exit(code);
      return;
    }
    case "help":
    case undefined:
      printModuleHelp(process.stdout.write.bind(process.stdout));
      return;
    default:
      process.stderr.write(`error: unknown skills command "${command}"\n\n`);
      printModuleHelp(process.stderr.write.bind(process.stderr));
      process.exit(2);
  }
}

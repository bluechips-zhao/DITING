#requires -Version 7.0
[CmdletBinding()]
param(
 [Parameter(Mandatory)][ValidateSet('Backup','Preview','Restore')][string]$Mode,
 [Parameter(Mandatory)][string]$Package,
 [string]$PgBin='',
 [string]$DsnEnv='ARTEX_PG_DSN',
 [string]$RestoreDsnEnv='DITING_RESTORE_DSN',
 [string]$DataDir='',
 [string]$SkillsDir='',
 [string]$ConfigFile='',
 [string]$SigningKey='',
 [string]$DestinationDir='',
 [switch]$IncludeLocalCredentials,
 [switch]$OfflineAcknowledged,
 [long]$MaxRestoreBytes=53687091200
)
$ErrorActionPreference='Stop'
$magic=[Text.Encoding]::ASCII.GetBytes('DITBKP01')
$packagePath=[IO.Path]::GetFullPath($Package)
$scratchRoot=Join-Path ([IO.Path]::GetTempPath()) 'diting-backup'
$stage=Join-Path $scratchRoot ([guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($stage)
$passphrase=[Environment]::GetEnvironmentVariable('DITING_BACKUP_PASSPHRASE')
$preserveStage=$false
if([string]::IsNullOrEmpty($passphrase) -or $passphrase.Length -lt 16){throw 'DITING_BACKUP_PASSPHRASE must contain at least 16 characters. Never pass it on the command line.'}

function Read-Exact([IO.Stream]$stream,[int]$length){
 $bytes=[byte[]]::new($length);$offset=0
 while($offset -lt $length){$n=$stream.Read($bytes,$offset,$length-$offset);if($n -le 0){throw 'Truncated backup package.'};$offset+=$n}
 return ,$bytes
}
function Get-Key([byte[]]$salt){
 return ,[Security.Cryptography.Rfc2898DeriveBytes]::Pbkdf2([Text.Encoding]::UTF8.GetBytes($passphrase),$salt,200000,[Security.Cryptography.HashAlgorithmName]::SHA256,32)
}
function Get-Aad([long]$index,[int]$length){return ,([byte[]]($magic+[BitConverter]::GetBytes($index)+[BitConverter]::GetBytes($length)))}
function Protect-Archive([string]$source,[string]$target){
 $inputStream=[IO.File]::OpenRead($source);$outputStream=[IO.File]::Open($target,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
 $salt=[Security.Cryptography.RandomNumberGenerator]::GetBytes(16);$aes=[Security.Cryptography.AesGcm]::new((Get-Key $salt),16)
 try{
  $outputStream.Write($magic);$outputStream.Write($salt);$buffer=[byte[]]::new(1048576);$index=0L
  while($true){$length=$inputStream.Read($buffer,0,$buffer.Length);$nonce=[Security.Cryptography.RandomNumberGenerator]::GetBytes(12);$tag=[byte[]]::new(16);$cipher=[byte[]]::new($length)
   $plain=[byte[]]::new($length);[Array]::Copy($buffer,$plain,$length)
   $aes.Encrypt($nonce,$plain,$cipher,$tag,(Get-Aad $index $length));$outputStream.Write([BitConverter]::GetBytes([int]$length));$outputStream.Write($nonce);$outputStream.Write($tag);$outputStream.Write($cipher);$index++
   if($length -eq 0){break}
  }
  $outputStream.Flush($true)
 }finally{$aes.Dispose();$inputStream.Dispose();$outputStream.Dispose()}
}
function Unprotect-Archive([string]$source,[string]$target){
 $inputStream=[IO.File]::OpenRead($source);$outputStream=[IO.File]::Open($target,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
 $aes=$null
 try{
  if([Text.Encoding]::ASCII.GetString((Read-Exact $inputStream 8)) -ne 'DITBKP01'){throw 'Unsupported backup format.'}
  $aes=[Security.Cryptography.AesGcm]::new((Get-Key (Read-Exact $inputStream 16)),16);$index=0L;$total=0L
  while($true){$length=[BitConverter]::ToInt32((Read-Exact $inputStream 4),0);if($length -lt 0 -or $length -gt 1048576){throw 'Invalid encrypted block length.'}
   $nonce=Read-Exact $inputStream 12;$tag=Read-Exact $inputStream 16;$cipher=Read-Exact $inputStream $length;$plain=[byte[]]::new($length)
   $aes.Decrypt($nonce,$cipher,$tag,$plain,(Get-Aad $index $length));$index++;$total+=$length
   if($total -gt $MaxRestoreBytes){throw 'Package exceeds restore size limit.'}
   if($length -eq 0){if($inputStream.ReadByte() -ne -1){throw 'Trailing package data.'};break}
   $outputStream.Write($plain)
  }
 }finally{if($aes){$aes.Dispose()};$inputStream.Dispose();$outputStream.Dispose()}
}
function Invoke-Pg([string]$program,[string]$connectionEnv,[string[]]$arguments){
 $dsn=[Environment]::GetEnvironmentVariable($connectionEnv);if(!$dsn){throw "Missing connection environment variable $connectionEnv."}
 $u=[Uri]$dsn;if($u.Scheme -notin @('postgres','postgresql')){throw 'Use a PostgreSQL connection URI.'}
 $auth=$u.UserInfo.Split(':',2);$psi=[Diagnostics.ProcessStartInfo]::new()
 $extension=if($IsWindows){'.exe'}else{''}
 $exe=if($PgBin){Join-Path ([IO.Path]::GetFullPath($PgBin)) ($program+$extension)}else{$program}
 $psi.FileName=$exe;$psi.UseShellExecute=$false;$psi.RedirectStandardOutput=$true;$psi.RedirectStandardError=$true;$psi.CreateNoWindow=$true
 $psi.Environment['PGHOST']=$u.Host;$psi.Environment['PGPORT']=if($u.Port -gt 0){[string]$u.Port}else{'5432'}
 $psi.Environment['PGUSER']=[Uri]::UnescapeDataString($auth[0]);$psi.Environment['PGPASSWORD']=if($auth.Count -gt 1){[Uri]::UnescapeDataString($auth[1])}else{''}
 $psi.Environment['PGDATABASE']=[Uri]::UnescapeDataString($u.AbsolutePath.TrimStart('/'))
 foreach($pair in $u.Query.TrimStart('?').Split('&')){$kv=$pair.Split('=',2);if($kv.Count -eq 2 -and $kv[0] -eq 'sslmode'){$psi.Environment['PGSSLMODE']=$kv[1]}}
 $psi.Environment['PGCONNECT_TIMEOUT']='10'
 foreach($arg in $arguments){[void]$psi.ArgumentList.Add($arg)}
 $process=[Diagnostics.Process]::Start($psi);$stdout=$process.StandardOutput.ReadToEndAsync();$stderr=$process.StandardError.ReadToEndAsync();$process.WaitForExit();$result=$stdout.GetAwaiter().GetResult();$errorText=$stderr.GetAwaiter().GetResult()
 try{if($process.ExitCode -ne 0){throw "$program failed: $errorText"};return $result.Trim()}finally{$process.Dispose()}
}
function Add-File([IO.Compression.ZipArchive]$archive,[string]$path,[string]$name){
 $before=Get-Item -LiteralPath $path;if(($before.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw "Refusing link/reparse point: $name"}
 $entry=$archive.CreateEntry($name,[IO.Compression.CompressionLevel]::Optimal);$inputStream=[IO.File]::Open($path,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read);$entryStream=$entry.Open()
 $hash=[Security.Cryptography.IncrementalHash]::CreateHash([Security.Cryptography.HashAlgorithmName]::SHA256)
 try{$buffer=[byte[]]::new(1048576);$length=0L;while(($n=$inputStream.Read($buffer,0,$buffer.Length)) -gt 0){$entryStream.Write($buffer,0,$n);$hash.AppendData($buffer,0,$n);$length+=$n};$digest=[Convert]::ToHexString($hash.GetHashAndReset()).ToLowerInvariant()}finally{$hash.Dispose();$entryStream.Dispose();$inputStream.Dispose()}
 $after=Get-Item -LiteralPath $path;if($after.Length -ne $before.Length -or $after.LastWriteTimeUtc -ne $before.LastWriteTimeUtc){throw "File changed during backup: $name"}
 return @{path=$name;bytes=$length;sha256=$digest}
}
function Add-Tree([IO.Compression.ZipArchive]$archive,[string]$directory,[string]$prefix){
 $root=[IO.Path]::GetFullPath($directory);if(!(Test-Path -LiteralPath $root -PathType Container)){throw "Missing source directory: $prefix"}
 if(((Get-Item -LiteralPath $root).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw "Refusing linked source root: $prefix"}
 $items=@(Get-ChildItem -LiteralPath $root -Force -Recurse);foreach($item in $items){if(($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw "Refusing linked source entry: $prefix"};if(!$item.PSIsContainer){$relative=[IO.Path]::GetRelativePath($root,$item.FullName).Replace('\','/');Add-File $archive $item.FullName ($prefix+'/'+$relative)}}
}
function Verify-Archive([IO.Compression.ZipArchive]$archive){
 $manifestEntry=$archive.GetEntry('manifest.json');if(!$manifestEntry -or $manifestEntry.Length -gt 16777216){throw 'Missing or excessive manifest.'}
 $reader=[IO.StreamReader]::new($manifestEntry.Open());try{$manifest=$reader.ReadToEnd()|ConvertFrom-Json}finally{$reader.Dispose()}
 if($manifest.format_version -ne 1 -or $manifest.consistency -ne 'offline_user_asserted'){throw 'Unsupported manifest.'}
 $entries=@{};$total=0L
 foreach($entry in $archive.Entries){if($entries.ContainsKey($entry.FullName)){throw 'Duplicate archive entry.'};$entries[$entry.FullName]=$entry
  if($entry.FullName -match '\\|(^|/)\.\.?(/|$)|:' -or $entry.FullName.StartsWith('/') -or $entry.FullName -notmatch '^(database\.dump|manifest\.json|data/|skills/|local/config\.json$|local/jwt\.key$)'){throw 'Unsafe archive path.'}
  $total+=$entry.Length;if($total -gt $MaxRestoreBytes){throw 'Expanded package exceeds size limit.'}
 }
 if($entries.Count -ne @($manifest.files).Count+1){throw 'Manifest inventory mismatch.'}
 foreach($file in $manifest.files){$entry=$entries[$file.path];if(!$entry -or $entry.Length -ne $file.bytes){throw "Missing/length mismatch: $($file.path)"};$stream=$entry.Open();try{$hash=[Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($stream)).ToLowerInvariant()}finally{$stream.Dispose()};if($hash -ne $file.sha256){throw "Checksum mismatch: $($file.path)"}}
 if(!$entries.ContainsKey('database.dump')){throw 'Database dump missing.'}
 return $manifest
}
try{
 if($Mode -eq 'Backup'){
  if(!$OfflineAcknowledged){throw 'Stop DITING and its proxy first, then use -OfflineAcknowledged. This provides database/file consistency.'}
  if(Test-Path -LiteralPath $packagePath){throw 'Output package exists; choose a new filename.'}
  if(!$DataDir){throw 'DataDir is required.'}
  foreach($root in @($DataDir,$SkillsDir)|Where-Object{$_}){$absolute=[IO.Path]::GetFullPath($root).TrimEnd([IO.Path]::DirectorySeparatorChar)+[IO.Path]::DirectorySeparatorChar;if($packagePath.StartsWith($absolute,[StringComparison]::OrdinalIgnoreCase)){throw 'Package must be outside source directories.'}}
  $dump=Join-Path $stage 'database.dump';[void](Invoke-Pg 'pg_dump' $DsnEnv @('--format=custom','--no-owner','--file',$dump))
  $zipPath=Join-Path $stage 'snapshot.zip';$zipStream=[IO.File]::Open($zipPath,[IO.FileMode]::CreateNew);$archive=[IO.Compression.ZipArchive]::new($zipStream,[IO.Compression.ZipArchiveMode]::Create)
  try{
   $files=@((Add-File $archive $dump 'database.dump'));$files+=@(Add-Tree $archive $DataDir 'data');if($SkillsDir){$files+=@(Add-Tree $archive $SkillsDir 'skills')}
   if($IncludeLocalCredentials){if($ConfigFile){$files+=Add-File $archive ([IO.Path]::GetFullPath($ConfigFile)) 'local/config.json'};if($SigningKey){$files+=Add-File $archive ([IO.Path]::GetFullPath($SigningKey)) 'local/jwt.key'}}
   $schemaFile=Join-Path $PSScriptRoot '../db/schema.sql'
   $schemaHash=if(Test-Path -LiteralPath $schemaFile){(Get-FileHash -LiteralPath $schemaFile -Algorithm SHA256).Hash}else{'unavailable'}
   $manifest=@{format_version=1;application='DITING';schema_sha256=$schemaHash;created_at=[DateTimeOffset]::UtcNow.ToString('o');consistency='offline_user_asserted';database='complete PostgreSQL custom dump';credentials='Database includes stored credentials; whole package encrypted. Local config/signing key included only on explicit selection.';local_credentials_selected=[bool]$IncludeLocalCredentials;files=$files}
   $entry=$archive.CreateEntry('manifest.json');$writer=[IO.StreamWriter]::new($entry.Open(),[Text.UTF8Encoding]::new($false));try{$writer.Write(($manifest|ConvertTo-Json -Depth 8))}finally{$writer.Dispose()}
  }finally{$archive.Dispose();$zipStream.Dispose()}
  $tempPackage=$packagePath+'.partial-'+[guid]::NewGuid().ToString('N');Protect-Archive $zipPath $tempPackage;Move-Item -LiteralPath $tempPackage -Destination $packagePath
  Write-Output ([pscustomobject]@{mode='Backup';package=$packagePath;files=$files.Count;sha256=(Get-FileHash -LiteralPath $packagePath -Algorithm SHA256).Hash;encrypted=$true})
 }else{
  $zipPath=Join-Path $stage 'verified.zip';Unprotect-Archive $packagePath $zipPath;$archive=[IO.Compression.ZipFile]::OpenRead($zipPath)
  try{
   $manifest=Verify-Archive $archive
   if($Mode -eq 'Preview'){Write-Output ([pscustomobject]@{mode='Preview';verified=$true;files=@($manifest.files).Count;inventory=$manifest.files;schema_sha256=$manifest.schema_sha256;bytes=($manifest.files|Measure-Object bytes -Sum).Sum;database=$manifest.database;local_credentials=$manifest.local_credentials_selected;restore_policy='New empty database and new empty destination directory only. No overwrite.'});return}
   if(!$OfflineAcknowledged){throw 'Restore requires the destination service to be stopped and -OfflineAcknowledged.'}
   if(!$DestinationDir){throw 'DestinationDir is required.'};$destination=[IO.Path]::GetFullPath($DestinationDir)
   if(Test-Path -LiteralPath $destination){throw 'Destination directory must not exist; existing files are never overwritten.'}
   $count=Invoke-Pg 'psql' $RestoreDsnEnv @('-X','-A','-t','-v','ON_ERROR_STOP=1','-c',"SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema' AND c.relkind IN ('r','p','v','m','S','f');")
   if($count -ne '0'){throw 'Destination database is not empty; restore refused.'}
   $restoreStage=Join-Path $stage 'restored';[void][IO.Directory]::CreateDirectory($restoreStage)
   foreach($file in $manifest.files){$target=Join-Path $restoreStage $file.path;$resolved=[IO.Path]::GetFullPath($target);if(!$resolved.StartsWith($restoreStage+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)){throw 'Restore path escaped staging directory.'};[void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($target));$source=$archive.GetEntry($file.path).Open();$outputStream=[IO.File]::Open($target,[IO.FileMode]::CreateNew);try{$source.CopyTo($outputStream)}finally{$source.Dispose();$outputStream.Dispose()}}
   [void](Invoke-Pg 'pg_restore' $RestoreDsnEnv @('--no-owner','--no-privileges','--single-transaction','--exit-on-error','--dbname',([Uri][Environment]::GetEnvironmentVariable($RestoreDsnEnv)).AbsolutePath.TrimStart('/'),(Join-Path $restoreStage 'database.dump')))
   $preserveStage=$true
   # Validate the explicitly named absolute destination before moving its staged tree.
   if([IO.Path]::GetFullPath($DestinationDir) -ne $destination -or (Test-Path -LiteralPath $destination)){throw 'Destination changed during restore; database restored but files remain staged.'}
   [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($destination));Move-Item -LiteralPath $restoreStage -Destination $destination
   $preserveStage=$false
   Write-Output ([pscustomobject]@{mode='Restore';verified=$true;destination=$destination;files=@($manifest.files).Count;database='restored atomically into the explicitly selected empty database'})
  }finally{$archive.Dispose()}
 }
}finally{
 $resolvedStage=[IO.Path]::GetFullPath($stage);$resolvedRoot=[IO.Path]::GetFullPath($scratchRoot).TrimEnd([IO.Path]::DirectorySeparatorChar)+[IO.Path]::DirectorySeparatorChar
 if($preserveStage){Write-Warning "Database restored; file staging retained for recovery: $resolvedStage"}elseif($resolvedStage.StartsWith($resolvedRoot,[StringComparison]::OrdinalIgnoreCase) -and (Test-Path -LiteralPath $resolvedStage)){Remove-Item -LiteralPath $resolvedStage -Recurse -Force}
 $passphrase=$null
}
